TEST?=$$(go list ./...)
RUN?=

# Build targets
.PHONY: build
build:
	@echo "Building all binaries..."
	go build -o bin/outpost ./cmd/outpost
	go build -o bin/outpost-server ./cmd/outpost-server
	go build -o bin/outpost-migrate-redis ./cmd/outpost-migrate-redis
	@echo "Binaries built in ./bin/"

build/goreleaser:
	goreleaser release -f ./build/.goreleaser.yaml --snapshot --clean

build/outpost:
	go build -o bin/outpost ./cmd/outpost

build/server:
	go build -o bin/outpost-server ./cmd/outpost-server

build/migrate-redis:
	go build -o bin/outpost-migrate-redis ./cmd/outpost-migrate-redis

install:
	@echo "Installing binaries to GOPATH/bin..."
	go install ./cmd/outpost
	go install ./cmd/outpost-server
	go install ./cmd/outpost-migrate-redis
	@echo "Installation complete"

clean:
	rm -f bin/outpost bin/outpost-server bin/outpost-migrate-redis

up:
	make up/deps
	make up/outpost

down:
	make down/outpost
	make down/deps

up/outpost:
	docker-compose -f build/dev/compose.yml --env-file .env up -d

down/outpost:
	docker-compose -f build/dev/compose.yml --env-file .env down

up/deps:
	docker-compose -f build/dev/deps/compose.yml --env-file .env up -d

down/deps:
	docker-compose -f build/dev/deps/compose.yml --env-file .env down

up/mqs:
	docker-compose -f build/dev/mqs/compose.yml up -d

down/mqs:
	docker-compose -f build/dev/mqs/compose.yml down

up/grafana:
	docker-compose -f build/dev/grafana/compose.yml up -d

down/grafana:
	docker-compose -f build/dev/grafana/compose.yml down

up/uptrace:
	docker-compose -f build/dev/uptrace/compose.yml up -d

down/uptrace:
	docker-compose -f build/dev/uptrace/compose.yml down

up/portal:
	cd internal/portal && npm install && npm run dev

up/azure:
	docker compose -f build/dev/azure/compose.yml up -d

down/azure:
	docker compose -f build/dev/azure/compose.yml down --volumes

up/test:
	docker-compose -f build/test/compose.yml up -d

down/test:
	docker-compose -f build/test/compose.yml down --volumes

up/test/rediscluster:
	@echo "Ensuring test network exists..."
	@docker network create outpost-test_default 2>/dev/null || true
	@UNAME_S=$$(uname -s); \
	if [ "$$UNAME_S" = "Darwin" ]; then \
		REDIS_IMAGE=neohq/redis-cluster:latest; \
		echo "Detected macOS, using neohq/redis-cluster image..."; \
	else \
		REDIS_IMAGE=grokzen/redis-cluster:7.2.4; \
		echo "Using grokzen/redis-cluster image..."; \
	fi; \
	REDIS_IMAGE=$$REDIS_IMAGE docker-compose -f build/test/redis-cluster-compose.yml up -d
	@echo "Starting Redis cluster and test runner containers..."
	@echo "  - Redis cluster: 6 nodes (3 masters + 3 replicas)"
	@echo "  - Test runner: Alpine container with Go code mounted"
	@echo ""
	@echo "Waiting for cluster to initialize..."
	@sleep 10
	@echo "Checking Redis cluster status:"
	@docker exec redis-cluster redis-cli -p 7000 cluster info | grep cluster_state || echo "Failed to check cluster status"
	@echo ""
	@echo "Test environment ready. Run: make test/e2e/rediscluster"

down/test/rediscluster:
	@echo "Stopping Redis cluster test environment..."
	@docker-compose -f build/test/redis-cluster-compose.yml down --volumes
	@echo "Redis cluster test environment stopped."

test/setup:
	@echo "To setup the test environment, run the following command:"
	@echo "$$ make up/test"
	@echo "$$ make up/azure"
	@echo ""
	@echo "Before running the tests, make sure to:"
	@echo "$$ export TESTINFRA=1 TESTAZURE=1"
	@echo ""

test:
	@if [ "$(RUN)" != "" ]; then \
		$(if $(TESTINFRA),TESTINFRA=$(TESTINFRA)) go test $(TEST) $(TESTARGS) -run "$(RUN)"; \
	else \
		$(if $(TESTINFRA),TESTINFRA=$(TESTINFRA)) go test $(TEST) $(TESTARGS); \
	fi

test/unit:
	$(if $(TESTINFRA),TESTINFRA=$(TESTINFRA)) go test $(TEST) $(TESTARGS) -short

test/integration:
	$(if $(TESTINFRA),TESTINFRA=$(TESTINFRA)) go test $(TEST) $(TESTARGS) -run "Integration"

test/e2e/rediscluster:
	@echo "Running Redis cluster e2e tests in Docker container..."
	@if ! docker ps | grep -q test-runner; then \
		echo "Error: test-runner container not running. Run 'make up/test/rediscluster' first."; \
		exit 1; \
	fi
	@docker exec test-runner sh -c "cd /app && go test ./cmd/e2e -v -run TestRedisClusterBasicSuite"
	@echo "Redis cluster e2e tests completed."

test/race:
	TESTRACE=1 go test $(TEST) $(TESTARGS) -race

test/coverage:
	go test $(TEST) $(TESTARGS) -coverprofile=coverage.out

test/coverage/html:
	go tool cover -html=coverage.out

docs/generate/config:
	go run cmd/configdocsgen/main.go

redis/debug:
	go run cmd/redis-debug/main.go $(ARGS)

network:
	docker network create outpost

logs:
	docker logs $$(docker ps -f name=outpost-${SERVICE} --format "{{.ID}}") -f $(ARGS)
