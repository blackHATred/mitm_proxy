package main

import (
	"log"
	"net/http"
)

func main() {
	proxy := new(Proxy)
	proxy.SetClient()

	log.Fatal(http.ListenAndServe(":8080", proxy))
}
