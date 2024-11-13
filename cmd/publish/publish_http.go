package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

const (
	ServerURL = "http://localhost:3333/api/v1/publish"
	APIKey    = "apikey"
)

func publishHTTP(body map[string]interface{}) error {
	log.Printf("[x] Publishing HTTP")

	// make HTTP POST request to the URL specified in the body

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal body to JSON: %w", err)
	}

	// Make HTTP POST request
	req, err := http.NewRequest("POST", ServerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+APIKey)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP POST request: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-200 status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}

	return nil
}
