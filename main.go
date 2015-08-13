package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
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

	"github.com/opendoor-labs/gothumb/Godeps/_workspace/src/github.com/DAddYE/vips"
	"github.com/opendoor-labs/gothumb/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/opendoor-labs/gothumb/Godeps/_workspace/src/github.com/rlmcpherson/s3gof3r"
)

var (
	maxAge           int
	securityKey      []byte
	resultBucketName string
	useRRS           bool

	httpClient   *http.Client
	resultBucket *s3gof3r.Bucket
)

func main() {
	securityKey = []byte(mustGetenv("SECURITY_KEY"))
	resultBucketName = mustGetenv("RESULT_STORAGE_BUCKET")

	if maxAgeStr := os.Getenv("MAX_AGE"); maxAgeStr != "" {
		var err error
		if maxAge, err = strconv.Atoi(maxAgeStr); err != nil {
			log.Fatal("invalid MAX_AGE setting")
		}
	}
	if rrs := os.Getenv("USE_RRS"); rrs == "true" || rrs == "1" {
		useRRS = true
	}

	keys, err := s3gof3r.EnvKeys()
	if err != nil {
		log.Fatal(err)
	}
	resultBucket = s3gof3r.New(s3gof3r.DefaultDomain, keys).Bucket(resultBucketName)
	resultBucket.Md5Check = false
	httpClient = resultBucket.Client

	router := httprouter.New()
	router.HEAD("/:signature/:size/*source", handleResize)
	router.GET("/:signature/:size/*source", handleResize)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
	log.Fatal(http.ListenAndServe(":"+port, router))
}

func handleResize(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Printf(req.Method + " " + req.URL.Path)
	sourceURL, err := url.Parse(strings.TrimPrefix(params.ByName("source"), "/"))
	if err != nil || !(sourceURL.Scheme == "http" || sourceURL.Scheme == "https") {
		http.Error(w, "invalid source URL", 400)
		return
	}

	sig := params.ByName("signature")
	pathToVerify := strings.TrimPrefix(req.URL.Path, "/"+sig+"/")
	if err := validateSignature(sig, pathToVerify); err != nil {
		http.Error(w, "invalid signature", 401)
		return
	}

	width, height, err := parseWidthAndHeight(params.ByName("size"))
	if err != nil {
		http.Error(w, "invalid height requested", 400)
		return
	}

	path := normalizePath(req.URL.Path)

	// try to get stored result
	r, h, err := getStoredResult(req.Method, path)
	if err != nil {
		log.Printf("getting stored result: %s", err)
		generateThumbnail(w, req.Method, path, sourceURL.String(), width, height)
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
		ContentType:   "image/jpeg", // TODO: use stored content type
		ContentLength: length,
		ETag:          strings.Trim(h.Get("Etag"), `"`),
		Path:          path,
	})
	if _, err = io.Copy(w, r); err != nil {
		log.Printf("copying from stored result: %s", err)
		http.Error(w, err.Error(), 500)
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

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
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
		copyHeader(w.Header(), resp.Header)
		io.Copy(w, resp.Body)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, fmt.Sprintf("invalid content type %q", contentType), 500)
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
		Quality:      95,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("resizing image: %s", err.Error()), 500)
		return
	}

	res := &result{
		ContentType:   "image/jpeg",
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

	go storeResult(res)
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
