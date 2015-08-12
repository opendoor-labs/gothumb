package main

import (
	"bytes"
	"crypto/hmac"
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

	"github.com/julienschmidt/httprouter"
	"github.com/nfnt/resize"
)

var securityKey []byte

func main() {
	securityKeyStr := os.Getenv("SECURITY_KEY")
	if securityKeyStr == "" {
		log.Fatal("missing SECURITY_KEY")
	}
	securityKey = []byte(securityKeyStr)

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

	resp, err := http.Get(sourceURL.String())
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
	// TODO(bgentry) set other headers
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	if req.Method == "HEAD" {
	} else {
		if _, err = buf.WriteTo(w); err != nil {
			log.Printf("writing buffer to response: %s", err)
		}
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
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
