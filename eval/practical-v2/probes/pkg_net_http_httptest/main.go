package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

func main() {
	rec := httptest.NewRecorder()
	http.NotFound(rec, httptest.NewRequest("GET", "/", nil))
	fmt.Println(rec.Code)
}
