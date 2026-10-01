package mqs

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
