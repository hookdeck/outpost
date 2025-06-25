package mqinfra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type infraGCPPubSub struct {
	cfg *MQInfraConfig
}

func (infra *infraGCPPubSub) Exist(ctx context.Context) (bool, error) {
	if infra.cfg == nil || infra.cfg.GCPPubSub == nil {
		return false, errors.New("failed assertion: cfg.GCPPubSub != nil") // IMPOSSIBLE
	}

	// Create client options
	var opts []option.ClientOption
	if infra.cfg.GCPPubSub.ServiceAccountCredentials != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(infra.cfg.GCPPubSub.ServiceAccountCredentials)))
	}

	// Create client
	client, err := pubsub.NewClient(ctx, infra.cfg.GCPPubSub.ProjectID, opts...)
	if err != nil {
		return false, fmt.Errorf("failed to create pubsub client: %w", err)
	}
	defer client.Close()

	// Check if main topic exists
	topicID := infra.cfg.GCPPubSub.TopicID
	topic := client.Topic(topicID)
	topicExists, err := topic.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if topic exists: %w", err)
	}
	if !topicExists {
		return false, nil
	}

	// Check if DLQ topic exists
	dlqTopicID := topicID + "-dlq"
	dlqTopic := client.Topic(dlqTopicID)
	dlqTopicExists, err := dlqTopic.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if DLQ topic exists: %w", err)
	}
	if !dlqTopicExists {
		return false, nil
	}

	// Check if DLQ subscription exists
	dlqSubID := dlqTopicID + "-sub"
	dlqSub := client.Subscription(dlqSubID)
	dlqSubExists, err := dlqSub.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if DLQ subscription exists: %w", err)
	}
	if !dlqSubExists {
		return false, nil
	}

	// Check if main subscription exists
	subID := infra.cfg.GCPPubSub.SubscriptionID
	sub := client.Subscription(subID)
	subExists, err := sub.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if subscription exists: %w", err)
	}
	if !subExists {
		return false, nil
	}

	return true, nil
}

func (infra *infraGCPPubSub) Declare(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.GCPPubSub == nil {
		return errors.New("failed assertion: cfg.GCPPubSub != nil") // IMPOSSIBLE
	}

	// Create client options
	var opts []option.ClientOption
	if infra.cfg.GCPPubSub.ServiceAccountCredentials != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(infra.cfg.GCPPubSub.ServiceAccountCredentials)))
	}

	// Create client
	client, err := pubsub.NewClient(ctx, infra.cfg.GCPPubSub.ProjectID, opts...)
	if err != nil {
		return fmt.Errorf("failed to create pubsub client: %w", err)
	}
	defer client.Close()

	// Create topic (if not exists)
	topicID := infra.cfg.GCPPubSub.TopicID
	topic := client.Topic(topicID)
	topicExists, err := topic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if topic exists: %w", err)
	}

	if !topicExists {
		topic, err = client.CreateTopic(ctx, topicID)
		if err != nil {
			// Check if the error is because the topic already exists
			if !isAlreadyExistsError(err) {
				return fmt.Errorf("failed to create topic: %w", err)
			}
			// If topic already exists, just use existing
			topic = client.Topic(topicID)
		}
	}

	// Create DLQ topic (if not exists)
	dlqTopicID := topicID + "-dlq"
	dlqTopic := client.Topic(dlqTopicID)
	dlqTopicExists, err := dlqTopic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if DLQ topic exists: %w", err)
	}

	if !dlqTopicExists {
		dlqTopic, err = client.CreateTopic(ctx, dlqTopicID)
		if err != nil {
			// Check if the error is because the topic already exists
			if !isAlreadyExistsError(err) {
				return fmt.Errorf("failed to create DLQ topic: %w", err)
			}
			// If topic already exists, just use existing
			dlqTopic = client.Topic(dlqTopicID)
		}
	}

	// Create DLQ subscription (if not exists)
	dlqSubID := dlqTopicID + "-sub"
	dlqSub := client.Subscription(dlqSubID)
	dlqSubExists, err := dlqSub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if DLQ subscription exists: %w", err)
	}

	if !dlqSubExists {
		dlqSubConfig := pubsub.SubscriptionConfig{
			Topic: dlqTopic,
		}
		_, err = client.CreateSubscription(ctx, dlqSubID, dlqSubConfig)
		if err != nil {
			// Check if the error is because the subscription already exists
			if !isAlreadyExistsError(err) {
				return fmt.Errorf("failed to create DLQ subscription: %w", err)
			}
		}
	}

	// Create main subscription with DLQ configuration
	subID := infra.cfg.GCPPubSub.SubscriptionID
	sub := client.Subscription(subID)
	subExists, err := sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if subscription exists: %w", err)
	}

	// Set visibility timeout (acknowledgement deadline)
	ackDeadline := 10 // default 10 seconds
	if infra.cfg.Policy.VisibilityTimeout > 0 {
		ackDeadline = infra.cfg.Policy.VisibilityTimeout
	}

	// Set retry limit (ensure minimum of 5 for GCP)
	maxDeliveryAttempts := 5 // GCP minimum value
	if infra.cfg.Policy.RetryLimit > 0 {
		// Adding 1 because GCP counts the initial delivery as attempt #1
		attempts := infra.cfg.Policy.RetryLimit + 1
		if attempts > 5 {
			maxDeliveryAttempts = attempts
		}
	}

	if !subExists {
		// Create new subscription with DLQ and retry settings
		subConfig := pubsub.SubscriptionConfig{
			Topic:       topic,
			AckDeadline: getDuration(ackDeadline),
			DeadLetterPolicy: &pubsub.DeadLetterPolicy{
				DeadLetterTopic:     dlqTopic.String(),
				MaxDeliveryAttempts: maxDeliveryAttempts,
			},
		}
		_, err = client.CreateSubscription(ctx, subID, subConfig)
		if err != nil {
			// Check if the error is because the subscription already exists
			if !isAlreadyExistsError(err) {
				return fmt.Errorf("failed to create subscription: %w", err)
			}
		}
	}
	// We're focusing on provisioning only for now, so we'll skip updating existing subscriptions

	return nil
}

func (infra *infraGCPPubSub) TearDown(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.GCPPubSub == nil {
		return errors.New("failed assertion: cfg.GCPPubSub != nil") // IMPOSSIBLE
	}

	// Create client options
	var opts []option.ClientOption
	if infra.cfg.GCPPubSub.ServiceAccountCredentials != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(infra.cfg.GCPPubSub.ServiceAccountCredentials)))
	}

	// Create client
	client, err := pubsub.NewClient(ctx, infra.cfg.GCPPubSub.ProjectID, opts...)
	if err != nil {
		return fmt.Errorf("failed to create pubsub client: %w", err)
	}
	defer client.Close()

	// Delete main subscription
	subID := infra.cfg.GCPPubSub.SubscriptionID
	sub := client.Subscription(subID)
	subExists, err := sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if subscription exists: %w", err)
	}
	if subExists {
		if err := sub.Delete(ctx); err != nil {
			return fmt.Errorf("failed to delete subscription: %w", err)
		}
	}

	// Delete DLQ subscription
	dlqTopicID := infra.cfg.GCPPubSub.TopicID + "-dlq"
	dlqSubID := dlqTopicID + "-sub"
	dlqSub := client.Subscription(dlqSubID)
	dlqSubExists, err := dlqSub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if DLQ subscription exists: %w", err)
	}
	if dlqSubExists {
		if err := dlqSub.Delete(ctx); err != nil {
			return fmt.Errorf("failed to delete DLQ subscription: %w", err)
		}
	}

	// Delete main topic
	topicID := infra.cfg.GCPPubSub.TopicID
	topic := client.Topic(topicID)
	topicExists, err := topic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if topic exists: %w", err)
	}
	if topicExists {
		if err := topic.Delete(ctx); err != nil {
			return fmt.Errorf("failed to delete topic: %w", err)
		}
	}

	// Delete DLQ topic
	dlqTopic := client.Topic(dlqTopicID)
	dlqTopicExists, err := dlqTopic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if DLQ topic exists: %w", err)
	}
	if dlqTopicExists {
		if err := dlqTopic.Delete(ctx); err != nil {
			return fmt.Errorf("failed to delete DLQ topic: %w", err)
		}
	}

	return nil
}

// getDuration converts seconds to time.Duration
func getDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

// Helper function to check if error is an "already exists" error
func isAlreadyExistsError(err error) bool {
	// Use Google Cloud's status codes from the grpc status package
	// to check for AlreadyExists error code
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	return ok && s.Code() == codes.AlreadyExists
}
