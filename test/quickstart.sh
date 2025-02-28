#!/bin/bash

TEST_SCRIPT_DIR=$(dirname "$0")
TEST_RUN_ID=$(date +%Y-%m-%d_%H%M%S)
TEST_RUN_DIR="$TEST_SCRIPT_DIR/runs/$TEST_RUN_ID"
DOCKER_COMPOSE_DIR="$TEST_RUN_DIR/examples/docker-compose"
TEST_API_KEY="this_is_a_test_api_key"

echo "Creating a new test run $TEST_RUN_DIR"

mkdir "$TEST_RUN_DIR"

git clone https://github.com/hookdeck/outpost.git "$TEST_RUN_DIR"
cp .env.example "$TEST_RUN_DIR/examples/docker-compose/.env"

cd "$DOCKER_COMPOSE_DIR"

wait_for_service() {
  local url=$1
  local timeout=6
  while ! curl --fail --silent "$url"; do
    printf '.'
    sleep 5
    ((timeout--))
    if [ $timeout -le 0 ]; then
      echo "Timeout reached. Exiting."
      exit 1
    fi
  done
}

# Update the <API_KEY> value within the new .env file
sed -i '' "s/apikey/$TEST_API_KEY/" .env

docker-compose -f compose.yml -f compose-rabbitmq.yml -f compose-postgres.yml up -d

# Wait until the services are running
echo "Waiting for the services to start"
wait_for_service "localhost:3333/api/v1/healthz"

echo ""
echo "✅ Services are up and running"

echo ""
echo "Creating a tenant"

# Create a tenant
curl --location --request PUT "localhost:3333/api/v1/$TEST_RUN_ID" \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $TEST_API_KEY"

# Create a webhook destination
echo ""
echo "Creating a webhook destination"

curl --location "localhost:3333/api/v1/$TEST_RUN_ID/destinations" \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $TEST_API_KEY" \
--data "{
  \"type\": \"webhook\",
  \"topics\": [\"*\"],
  \"config\": {
    \"url\": \"http://host.docker.internal:4444/webhook/$TEST_RUN_ID\"
  }
}"

# Start a simple HTTP server in the background
printf "\n\nStarting a simple HTTP server in the background"
touch "server.log"
python3 "../../../../server.py" 4444 > "server.log" 2>&1 &
printf "\nWaiting for the test server to start..."
wait_for_service "localhost:4444"
printf "\n✅ Test server started"

# Capture the server's PID to kill it later
SERVER_PID=$!
# Capture the killing of the script and kill the server
trap "kill $SERVER_PID; echo 'Script interrupted. Killing the server...'; exit 1" INT TERM

# Publish an event
printf "\n\nPublishing an event"

EVENT_BODY='{"user_id":"userid"}'

curl --location 'localhost:3333/api/v1/publish' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $TEST_API_KEY" \
--data "{
    \"tenant_id\": \"$TEST_RUN_ID\",
    \"topic\": \"user.created\",
    \"eligible_for_retry\": true,
    \"metadata\": {
      \"meta\": \"data\"
    },
    \"data\": $EVENT_BODY
}"

# Example condition: wait for a specific log entry
printf "\nWaiting for the expected event to reach test server for test run $TEST_RUN_ID..."
while ! grep -q "$EVENT_BODY" "server.log"; do
  printf '.'
  sleep 5
done
echo ""
echo "✅ Received the expected request. Success! Exiting."

# Now kill the server
kill $SERVER_PID
