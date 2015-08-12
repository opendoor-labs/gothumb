package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/nfnt/resize"
	"github.com/rlmcpherson/s3gof3r"
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
	if rrs := os.Getenv("useRRS"); rrs == "true" || rrs == "1" {
		useRRS = true
	}

	keys, err := s3gof3r.EnvKeys()
	if err != nil {
		log.Fatal(err)
	}
	resultBucket = s3gof3r.New(s3gof3r.DefaultDomain, keys).Bucket(resultBucketName)
	resultBucket.Md5Check = true
	httpClient = resultBucket.Client

	router := httprouter.New()
	router.HEAD("/:signature/:size/*source", handleResize)
	router.GET("/:signature/:size/*source", handleResize)
	log.Fatal(http.ListenAndServe(":8888", router))
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

	// TODO(bgentry): normalize path. Support for custom root path? ala RESULT_STORAGE_AWS_STORAGE_ROOT_PATH

	// try to get stored result
	r, h, err := resultBucket.GetReader(req.URL.Path, nil)
	if err != nil {
		generateThumbnail(w, req, sourceURL.String(), width, height)
		return
	}

	// return stored result
	w.Header().Set("Content-Type", "image/jpeg") // TODO: use stored content type
	w.Header().Set("Content-Length", h.Get("Content-Length"))
	w.Header().Set("ETag", h.Get("Etag"))
	setCacheHeaders(w)
	if _, err = io.Copy(w, r); err != nil {
		fmt.Printf("copying from stored result: %s", err)
		http.Error(w, err.Error(), 500)
		return
	}
}

func generateThumbnail(w http.ResponseWriter, req *http.Request, sourceURL string, width, height uint) {
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

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	imgResized := resize.Resize(width, height, img, resize.Bicubic)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, imgResized, nil); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Header().Set("ETag", `"`+computeHexMD5(buf.Bytes())+`"`)
	setCacheHeaders(w)
	if req.Method == "HEAD" {
		return
	} else {
		if _, err = buf.WriteTo(w); err != nil {
			log.Printf("writing buffer to response: %s", err)
		}
	}
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

func mustGetenv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("missing %s env", name)
	}
	return value
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

func validateSignature(sig, pathPart string) error {
	h := hmac.New(sha1.New, securityKey)
	if _, err := h.Write([]byte(pathPart)); err != nil {
		return err
	}
	actualSig := base64.StdEncoding.EncodeToString(h.Sum(nil))
	// constant-time string comparison
	if subtle.ConstantTimeCompare([]byte(sig), []byte(actualSig)) != 1 {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
