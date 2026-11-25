package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x/{id}", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.PathValue("id"))) })
	fmt.Println(http.StatusText(404))
}
