package main

import (
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
	mux.Handle("/publish", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handlePublish(w, r)
	}))
	mux.Handle("/declare", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleDeclare(w, r)
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
		port = "5555"
	}
	return port
}
