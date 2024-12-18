package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func handlePublish(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var err error
	switch r.URL.Query().Get("method") {
	case "aws_sqs":
		err = publishAWS(body)
	case "rabbitmq":
		err = publishRabbitMQ(body)
	case "http":
		fallthrough
	default:
		err = publishHTTP(body)
	}

	if err != nil {
		log.Printf("\t%s\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
}
