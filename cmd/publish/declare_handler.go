package main

import (
	"log"
	"net/http"
)

func handleDeclare(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.URL.Query().Get("method") {
	case "aws":
		err = declareAWS()
	case "rabbitmq":
		// err = declareRabbitMQ(body)
	case "http":
		fallthrough
	default:
		// err = declareHTTP(body)
	}

	if err != nil {
		log.Printf("\t%s\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
}
