package main

import (
	"log"
	"net/http"
)

func handleDeclare(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.URL.Query().Get("method") {
	case "aws_sqs":
		err = declareAWS()
	case "azure_servicebus":
		err = declareAzureServiceBus()
	case "gcp_pubsub":
		err = declareGCP()
	case "rabbitmq":
		err = declareRabbitMQ()
	case "http":
		log.Println("[*] Declare HTTP - nothing to declare")
	default:
		log.Printf("[?] Unsupport publishmq method: %s", r.URL.Query().Get("method"))
	}

	if err != nil {
		log.Printf("\t%s\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
}
