package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello")
	})
	if err := http.ListenAndServe(os.Args[1], nil); err != nil {
		fmt.Println(err)
	}
}
