package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, proceeding without it")
	}

	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:3333"
		log.Printf("SERVER_URL not set, defaulting to %s", serverURL)
	}

	if adminAPIKey == "" {
		log.Println("Warning: ADMIN_API_KEY environment variable not set. Some examples might fail.")
	}

	if len(os.Args) < 2 {
		log.Println("Usage: go run . <example_name>")
		log.Println("Available examples: manage, auth, create-destination, publish-event")
		os.Exit(1)
	}

	exampleToRun := os.Args[1]

	switch exampleToRun {
	case "manage":
		if adminAPIKey == "" {
			log.Fatal("ADMIN_API_KEY environment variable must be set to run the 'manage' example.")
		}
		log.Println("--- Running Manage Outpost Resources Example ---")
		manageOutpostResources(adminAPIKey, serverURL)
	case "auth":
		log.Println("--- Running Auth Example ---")
		runAuthExample()
	case "create-destination":
		if adminAPIKey == "" {
			log.Fatal("ADMIN_API_KEY environment variable must be set to run the 'create-destination' example.")
		}
		log.Println("--- Running Create Destination Example ---")
		runCreateDestinationExample()
	case "publish-event":
		if adminAPIKey == "" {
			log.Fatal("ADMIN_API_KEY environment variable must be set to run the 'publish-event' example.")
		}
		log.Println("--- Running Publish Event Example ---")
		runPublishEventExample()
	default:
		log.Printf("Unknown example: %s\n", exampleToRun)
		log.Println("Available examples: manage, auth, create-destination, publish-event")
		os.Exit(1)
	}
}
