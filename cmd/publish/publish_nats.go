package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Defaults match examples/docker-compose/compose-publish-nats.yml.
const (
	natsDefaultURL      = "nats://localhost:4222"
	natsDefaultStream   = "outpost-publish"
	natsDefaultSubject  = "outpost.publish"
	natsDefaultConsumer = "outpost"
)

func natsURL() string {
	if v := os.Getenv("PUBLISH_NATS_URL"); v != "" {
		return v
	}
	return natsDefaultURL
}

func natsSubject() string {
	if v := os.Getenv("PUBLISH_NATS_SUBJECT"); v != "" {
		return v
	}
	return natsDefaultSubject
}

func natsStream() string {
	if v := os.Getenv("PUBLISH_NATS_STREAM"); v != "" {
		return v
	}
	return natsDefaultStream
}

func natsConsumer() string {
	if v := os.Getenv("PUBLISH_NATS_CONSUMER"); v != "" {
		return v
	}
	return natsDefaultConsumer
}

func natsCredsFile() string {
	return os.Getenv("PUBLISH_NATS_CREDS")
}

func publishNATS(body map[string]interface{}) error {
	log.Printf("[x] Publishing NATS JetStream")

	opts := []nats.Option{nats.Name("outpost-publish-dev")}
	if creds := natsCredsFile(); creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}

	nc, err := nats.Connect(natsURL(), opts...)
	if err != nil {
		return err
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = js.Publish(ctx, natsSubject(), payload)
	return err
}

func declareNATS() error {
	log.Printf("[*] Declaring NATS JetStream Publish infra")

	opts := []nats.Option{nats.Name("outpost-publish-dev-declare")}
	if creds := natsCredsFile(); creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}

	nc, err := nats.Connect(natsURL(), opts...)
	if err != nil {
		return err
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      natsStream(),
		Subjects:  []string{natsSubject()},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return err
	}

	_, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       natsConsumer(),
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natsSubject(),
	})
	return err
}
