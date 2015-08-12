package main

import (
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/nfnt/resize"
)

func main() {
	router := httprouter.New()
	router.GET("/:size/*source", handleResize)
	log.Fatal(http.ListenAndServe(":8888", router))
}

func handleResize(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Printf(req.Method + " " + req.URL.Path)
	sourceURL, err := url.Parse(strings.TrimPrefix(params.ByName("source"), "/"))
	if err != nil || !(sourceURL.Scheme == "http" || sourceURL.Scheme == "https") {
		http.Error(w, "invalid source URL", 400)
		return
	}

	sizeParts := strings.Split(params.ByName("size"), "x")
	if len(sizeParts) != 2 {
		http.Error(w, "invalid width requested", 400)
		return
	}
	width, err := strconv.ParseUint(sizeParts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid width requested", 400)
		return
	}
	height, err := strconv.ParseUint(sizeParts[1], 10, 64)
	if err != nil {
		http.Error(w, "invalid height requested", 400)
		return
	}

	resp, err := http.Get(sourceURL.String())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

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

	imgResized := resize.Resize(uint(width), uint(height), img, resize.Bicubic)
	w.Header().Set("Content-Type", "image/jpeg")
	// TODO(bgentry) set other headers
	jpeg.Encode(w, imgResized, nil)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
