package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/DAddYE/vips"
	"github.com/julienschmidt/httprouter"
	"github.com/rlmcpherson/s3gof3r"
)

var (
	listenInterface  string
	maxAge           int
	securityKey      []byte
	resultBucketName string
	useRRS           bool
	unsafeMode       bool

	httpClient   = http.DefaultClient
	resultBucket *s3gof3r.Bucket
)

type ByteSize int64

const (
	_           = iota // ignore first value by assigning to blank identifier
	KB ByteSize = 1 << (10 * iota)
	MB
)

func main() {
	log.SetFlags(0) // hide timestamps from Go logs

	parseFlags()

	resultBucketName = os.Getenv("RESULT_STORAGE_BUCKET")
	if resultBucketName != "" {
		keys, err := s3gof3r.EnvKeys()
		if err != nil {
			log.Fatal(err)
		}
		resultBucket = s3gof3r.New(s3gof3r.DefaultDomain, keys).Bucket(resultBucketName)
		resultBucket.Concurrency = 4
		resultBucket.PartSize = int64(2 * MB)
		resultBucket.Md5Check = false
		httpClient = resultBucket.Client

		if rrs := os.Getenv("USE_RRS"); rrs == "true" || rrs == "1" {
			useRRS = true
		}
	}

	router := httprouter.New()
	router.HEAD("/:signature/:size/*source", handleResize)
	router.GET("/:signature/:size/*source", handleResize)
	log.Fatal(http.ListenAndServe(listenInterface, router))
}

func parseFlags() {
	securityKeyStr := ""

	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}

	if maxAgeStr := os.Getenv("MAX_AGE"); maxAgeStr != "" {
		var err error
		if maxAge, err = strconv.Atoi(maxAgeStr); err != nil {
			log.Fatal("invalid MAX_AGE setting")
		}
	}

	flag.StringVar(&listenInterface, "l", ":"+port, "listen address")
	flag.IntVar(&maxAge, "max-age", maxAge, "the maximum HTTP caching age to use on returned images")
	flag.StringVar(&securityKeyStr, "k", os.Getenv("SECURITY_KEY"), "security key")
	flag.BoolVar(&unsafeMode, "unsafe", false, "whether to allow /unsafe URLs")

	flag.Parse()

	if securityKeyStr == "" && !unsafeMode {
		log.Fatalf("must provide a security key with -k or allow unsafe URLs")
	}
	securityKey = []byte(securityKeyStr)
}

func handleResize(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	reqPath := req.URL.EscapedPath()
	log.Printf("%s %s", req.Method, reqPath)
	sourceURL, err := url.Parse(strings.TrimPrefix(params.ByName("source"), "/"))
	if err != nil || !(sourceURL.Scheme == "http" || sourceURL.Scheme == "https") {
		http.Error(w, "invalid source URL", 400)
		return
	}

	sig := params.ByName("signature")
	pathToVerify := strings.TrimPrefix(reqPath, "/"+sig+"/")
	if err := validateSignature(sig, pathToVerify); err != nil {
		http.Error(w, "invalid signature", 401)
		return
	}

	width, height, err := parseWidthAndHeight(params.ByName("size"))
	if err != nil {
		http.Error(w, "invalid height requested", 400)
		return
	}

	resultPath := normalizePath(strings.TrimPrefix(reqPath, "/"+sig))

	// TODO(bgentry): everywhere that switches on resultBucket should switch on
	// something like resultStorage instead.
	if resultBucket == nil {
		// no result storage, just generate the thumbnail
		generateThumbnail(w, req.Method, resultPath, sourceURL.String(), width, height)
		return
	}

	// try to get stored result
	r, h, err := getStoredResult(req.Method, resultPath)
	if err != nil {
		log.Printf("getting stored result: %s", err)
		generateThumbnail(w, req.Method, resultPath, sourceURL.String(), width, height)
		return
	}
	defer r.Close()

	// return stored result
	length, err := strconv.Atoi(h.Get("Content-Length"))
	if err != nil {
		log.Printf("invalid result content-length: %s", err)
		// TODO: try to generate instead of erroring w/ 500?
		http.Error(w, err.Error(), 500)
		return
	}

	setResultHeaders(w, &result{
		ContentType:   h.Get("Content-Type"),
		ContentLength: length,
		ETag:          strings.Trim(h.Get("Etag"), `"`),
		Path:          resultPath,
	})
	if _, err = io.Copy(w, r); err != nil {
		log.Printf("copying from stored result: %s", err)
		return
	}
	if err = r.Close(); err != nil {
		log.Printf("closing stored result copy: %s", err)
	}
}

type result struct {
	Data          []byte
	ContentType   string
	ContentLength int
	ETag          string
	Path          string
}

func computeHexMD5(data []byte) string {
	h := md5.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func generateThumbnail(w http.ResponseWriter, rmethod, rpath string, sourceURL string, width, height uint) {
	log.Printf("generating %s", rpath)
	resp, err := httpClient.Get(sourceURL)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("unexpected status code from source: %d", resp.StatusCode)
		http.Error(w, "", resp.StatusCode)
		return
	}

	img, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	buf, err := vips.Resize(img, vips.Options{
		Height:       int(height),
		Width:        int(width),
		Crop:         true,
		Interpolator: vips.BICUBIC,
		Gravity:      vips.CENTRE,
		Quality:      50,
	})
	if err != nil {
		responseCode := 500
		if err.Error() == "unknown image format" {
			responseCode = 400
		}
		http.Error(w, fmt.Sprintf("resizing image: %s", err.Error()), responseCode)
		return
	}

	res := &result{
		ContentType:   "image/jpeg", // TODO: support PNGs as well
		ContentLength: len(buf),
		Data:          buf, // TODO: check if I need to copy this
		ETag:          computeHexMD5(buf),
		Path:          rpath,
	}
	setResultHeaders(w, res)
	if rmethod != "HEAD" {
		if _, err = w.Write(buf); err != nil {
			log.Printf("writing buffer to response: %s", err)
		}
	}

	if resultBucket != nil {
		go storeResult(res)
	}
}

// caller is responsible for closing the returned ReadCloser
func getStoredResult(method, path string) (io.ReadCloser, http.Header, error) {
	if method != "HEAD" {
		return resultBucket.GetReader(path, nil)
	}

	s3URL := fmt.Sprintf("https://%s.s3.amazonaws.com%s", resultBucketName, path)
	req, err := http.NewRequest(method, s3URL, nil)
	if err != nil {
		return nil, nil, err
	}

	resultBucket.Sign(req)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// TODO: drain res.Body to ioutil.Discard before closing?
		res.Body.Close()
		return nil, nil, fmt.Errorf("unexpected status code %d", res.StatusCode)
	}
	res.Header.Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	return res.Body, res.Header, err
}

func mustGetenv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("missing %s env", name)
	}
	return value
}

func normalizePath(p string) string {
	// TODO(bgentry): Support for custom root path? ala RESULT_STORAGE_AWS_STORAGE_ROOT_PATH
	return path.Clean(p)
}

func parseWidthAndHeight(str string) (width, height uint, err error) {
	sizeParts := strings.Split(str, "x")
	if len(sizeParts) != 2 {
		err = fmt.Errorf("invalid size requested")
		return
	}
	width64, err := strconv.ParseUint(sizeParts[0], 10, 64)
	if err != nil {
		err = fmt.Errorf("invalid width requested")
		return
	}
	height64, err := strconv.ParseUint(sizeParts[1], 10, 64)
	if err != nil {
		err = fmt.Errorf("invalid height requested")
		return
	}
	return uint(width64), uint(height64), nil
}

func setCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d,public", maxAge))
	w.Header().Set("Expires", time.Now().UTC().Add(time.Duration(maxAge)*time.Second).Format(http.TimeFormat))
}

func setResultHeaders(w http.ResponseWriter, result *result) {
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(result.ContentLength))
	w.Header().Set("ETag", `"`+result.ETag+`"`)
	setCacheHeaders(w)
}

func storeResult(res *result) {
	h := make(http.Header)
	h.Set("Content-Type", res.ContentType)
	if useRRS {
		h.Set("x-amz-storage-class", "REDUCED_REDUNDANCY")
	}
	w, err := resultBucket.PutWriter(res.Path, h, nil)
	if err != nil {
		log.Printf("storing result for %s: %s", res.Path, err)
		return
	}
	defer w.Close()
	if _, err = w.Write(res.Data); err != nil {
		log.Printf("storing result for %s: %s", res.Path, err)
		return
	}
	if err = w.Close(); err != nil {
		log.Printf("storing result for %s: %s", res.Path, err)
	}
}

func validateSignature(sig, pathPart string) error {
	if unsafeMode && sig == "unsafe" {
		return nil
	}

	h := hmac.New(sha1.New, securityKey)
	if _, err := h.Write([]byte(pathPart)); err != nil {
		return err
	}
	actualSig := base64.URLEncoding.EncodeToString(h.Sum(nil))
	// constant-time string comparison
	if subtle.ConstantTimeCompare([]byte(sig), []byte(actualSig)) != 1 {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
