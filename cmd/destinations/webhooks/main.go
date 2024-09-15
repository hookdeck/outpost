package main

import (
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	if err := run(os.Getenv); err != nil {
		panic(err)
	}
}

func run(getenv func(string) string) error {
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fullPath := r.URL.Path
		if r.URL.RawQuery != "" {
			fullPath += "?" + r.URL.RawQuery
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[x] %s %s failed to read request body: %s", r.Method, fullPath, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		log.Printf("[x] %s %s %s", r.Method, fullPath, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	server := &http.Server{
		Addr:    ":" + getPort(getenv),
		Handler: mux,
	}
	log.Println("[*] Server listening on port " + server.Addr)
	if err := server.ListenAndServe(); err != nil {
		return err
	}
	return nil
}

func getPort(getenv func(string) string) string {
	port := getenv("PORT")
	if port == "" {
		port = "4444"
	}
	return port
}
