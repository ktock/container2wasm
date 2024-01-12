package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) == 2 {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "hello")
		})
	} else if len(os.Args) > 2 {
		http.Handle("/", http.FileServer(http.Dir(os.Args[2])))
	} else {
		panic("specify args")
	}
	if err := http.ListenAndServe(os.Args[1], nil); err != nil {
		fmt.Println(err)
	}
}
