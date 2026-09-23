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

func NATSQueueConfig(q Queue) *NATSConfig {
	return q.(*NATSQueue).config
}
