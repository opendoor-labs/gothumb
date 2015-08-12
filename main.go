package main

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/julienschmidt/httprouter"
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
	w.Write([]byte("ok\n"))
}
