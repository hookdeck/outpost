#!/bin/bash

# Make the script executable with: chmod +x start-rabbitmq.sh

echo "Starting publish RabbitMQ container..."
docker-compose -f compose-publish-queues.yml down
docker-compose -f compose-publish-queues.yml up -d

echo "Waiting for RabbitMQ to start..."
sleep 10

echo "Checking container status..."
docker ps | grep publish_rabbitmq

echo "Checking RabbitMQ logs..."
docker logs publish_rabbitmq

echo "RabbitMQ should now be available at:"
echo "- Management UI: http://localhost:15673"
echo "- AMQP: localhost:5673"

echo "Testing management interface connectivity..."
curl -s -o /dev/null -w "%{http_code}" http://localhost:15673 || echo "Cannot connect to management interface"
