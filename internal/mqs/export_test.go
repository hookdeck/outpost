package mqs

import nativepubsub "cloud.google.com/go/pubsub"

func ForceCloseRabbitMQConnection(q Queue) error {
	rq := q.(*RabbitMQQueue)
	rq.mu.Lock()
	conn := rq.conn
	rq.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

func RabbitMQConnectionClosed(q Queue) bool {
	return q.(*RabbitMQQueue).connectionLost()
}

func AWSQueueURL(q Queue) string {
	aq := q.(*AWSQueue)
	aq.mu.Lock()
	defer aq.mu.Unlock()
	return aq.sqsQueueURL
}

func NATSQueueConfig(q Queue) *NATSConfig {
	return q.(*NATSQueue).config
}

func LimitBytes(sub Subscription, maxBytes int64) Subscription {
	return limitBytes(sub, maxBytes)
}

func IsByteLimited(sub Subscription) bool {
	_, ok := sub.(*byteLimitSubscription)
	return ok
}

// ByteLimitWaiting reports whether a received message is waiting for bytes.
func ByteLimitWaiting(sub Subscription) bool {
	s, ok := sub.(*byteLimitSubscription)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.released != nil
}

func GCPReceiveSettings(q Queue, opts ...SubscribeOption) nativepubsub.ReceiveSettings {
	var rs nativepubsub.ReceiveSettings
	q.(*GCPPubSubQueue).configureReceive(&rs, ApplySubscribeOptions(opts))
	return rs
}
