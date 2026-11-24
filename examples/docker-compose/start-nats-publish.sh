#!/bin/bash

# Make the script executable with: chmod +x start-nats-publish.sh

echo "Starting publish NATS JetStream container..."
docker-compose -f compose-publish-nats.yml down
docker-compose -f compose-publish-nats.yml up -d

echo "Waiting for NATS to start..."
sleep 3

echo "Checking container status..."
docker ps | grep publish_nats

echo "NATS should now be available at:"
echo "- Client: nats://localhost:4222"
echo "- Monitoring: http://localhost:8222"

echo "Provision the stream + consumer used by the publish dev helper:"
echo "  curl -X POST 'http://localhost:5555/declare?method=nats'"
echo ""
echo "Publish a test event:"
echo "  curl -X POST 'http://localhost:5555/publish?method=nats' \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"tenant_id\":\"acme\",\"topic\":\"user.created\",\"data\":{\"id\":1}}'"
