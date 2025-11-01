// main.go
package main

import (
	"fmt"
	"net/http"
)

var count = 0

func handler(w http.ResponseWriter, r *http.Request) {
	count++
	fmt.Fprintln(w, "Hello from Go in K8s!  ", count)
}

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
