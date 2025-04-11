package main

import (
	"context"
	"encoding/json"
	"log"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	GCPEndpoint         = "localhost:8085"
	GCPProjectID        = "test"
	GCPPublishTopicName = "outpost-publish"
	GCPPublishSubName   = "outpost-publish-sub"
)

func publishGCP(body map[string]interface{}) error {
	log.Printf("[x] Publishing GCP PubSub")

	ctx := context.Background()
	client, err := getGCPClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	topic := client.Topic(GCPPublishTopicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		log.Printf("[!] Topic %s does not exist. Creating it first.", GCPPublishTopicName)
		if err := declareGCP(); err != nil {
			return err
		}
		topic = client.Topic(GCPPublishTopicName)
	}

	messageBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	result := topic.Publish(ctx, &pubsub.Message{
		Data: messageBody,
		Attributes: map[string]string{
			"source": "outpost-publish",
		},
	})

	_, err = result.Get(ctx)
	if err != nil {
		return err
	}

	log.Printf("[x] Published message to GCP PubSub topic %s", GCPPublishTopicName)
	return nil
}

func declareGCP() error {
	log.Printf("[*] Declaring GCP Publish infra")
	ctx := context.Background()
	client, err := getGCPClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	// Create topic if it doesn't exist
	topic, err := client.CreateTopic(ctx, GCPPublishTopicName)
	if err != nil {
		if err.Error() == "rpc error: code = AlreadyExists desc = Topic already exists" {
			log.Printf("[*] Topic %s already exists", GCPPublishTopicName)
		} else {
			return err
		}
	} else {
		log.Printf("[*] Topic %s created successfully", topic.ID())
	}

	// Create subscription if it doesn't exist
	_, err = client.CreateSubscription(ctx, GCPPublishSubName, pubsub.SubscriptionConfig{
		Topic: topic,
	})
	if err != nil {
		if err.Error() == "rpc error: code = AlreadyExists desc = Subscription already exists" {
			log.Printf("[*] Subscription %s already exists", GCPPublishSubName)
		} else {
			return err
		}
	} else {
		log.Printf("[*] Subscription %s created successfully", GCPPublishSubName)
	}

	return nil
}

func getGCPClient(ctx context.Context) (*pubsub.Client, error) {
	client, err := pubsub.NewClient(
		ctx,
		GCPProjectID,
		option.WithEndpoint(GCPEndpoint),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		return nil, err
	}
	return client, nil
}
