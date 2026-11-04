TEST?=./internal/...
RUN?=

# Build targets
.PHONY: build
build:
	@echo "Checking formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Formatting issues found in:"; \
		gofmt -l .; \
		echo "Run 'gofmt -w .' to fix"; \
		exit 1; \
	fi
	@echo "Building all binaries..."
	go build -tags nomsgpack -o bin/outpost ./cmd/outpost
	go build -tags nomsgpack -o bin/outpost-server ./cmd/outpost-server
	@echo "Binaries built in ./bin/"

build/goreleaser:
	goreleaser release -f ./build/.goreleaser.yaml --snapshot --clean --skip=sign

build/outpost:
	go build -tags nomsgpack -o bin/outpost ./cmd/outpost

build/server:
	go build -tags nomsgpack -o bin/outpost-server ./cmd/outpost-server

install:
	@echo "Installing binaries to GOPATH/bin..."
	go install -tags nomsgpack ./cmd/outpost
	go install -tags nomsgpack ./cmd/outpost-server
	@echo "Installation complete"

clean:
	rm -f bin/outpost bin/outpost-server

up:
	./build/dev/dev.sh up

down:
	./build/dev/dev.sh down

nuke:
	./build/dev/dev.sh down --volumes

health:
	@WAIT="$(WAIT)" ./build/dev/health.sh

smoke:
	./build/dev/smoke.sh

# Run portal natively (vite hot reload). Portal is also available as a
# containerized service via `make up` — use this target only when you want
# the faster native dev loop.
up/portal:
	cd internal/portal && npm install && npm run dev

# Test infrastructure for TESTINFRA=1 runs: the services the default test run
# needs. With TESTDEST=1 or TESTCOMPAT=1 it also starts the destination stack.
# --env-file supplies the image pins; see the comment at the top of compose.yml.
up/test:
	docker-compose --env-file .env.test -f build/test/compose.yml up -d
	@if [ "$(TESTDEST)" = "1" ] || [ "$(TESTCOMPAT)" = "1" ]; then $(MAKE) --no-print-directory up/dest; fi

down/test:
	docker-compose --env-file .env.test -f build/test/compose.yml down --volumes

# Destination stack: brokers Outpost delivers to (build/dest/compose.yml), used
# by manual testing against `make up`, cmd/destinations and the TESTDEST=1 /
# TESTCOMPAT=1 tests. DEST picks brokers; default all.
DEST_ALL := rabbitmq aws gcp kafka
DEST ?= $(DEST_ALL)
comma := ,

up/dest:
	@for d in $(DEST); do case " $(DEST_ALL) " in *" $$d "*) ;; *) echo "unknown DEST '$$d' (choose from: $(DEST_ALL))"; exit 2;; esac; done
	@docker network create outpost-dest >/dev/null 2>&1 || true
	COMPOSE_PROFILES="$(subst $() ,$(comma),$(strip $(DEST)))" docker-compose --env-file .env.test -f build/dest/compose.yml up -d

down/dest:
	COMPOSE_PROFILES="*" docker-compose --env-file .env.test -f build/dest/compose.yml down --volumes
	@docker network rm outpost-dest >/dev/null 2>&1 || true

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
	@echo "To set up the test environment:"
	@echo "$$ go install gotest.tools/gotestsum@latest"
	@echo "$$ export TESTINFRA=1    # use the services from make up/test"
	@echo "$$ make up/test          # ClickHouse, Postgres, NATS, mock server"
	@echo "$$ make test"
	@echo ""
	@echo "Optional test groups (export before make up/test and make test). TESTDEST ⊂ TESTCOMPAT; TESTAZURE is separate:"
	@echo "  TESTDEST=1     destination providers: RabbitMQ, AWS, GCP Pub/Sub, Kafka (starts make up/dest)"
	@echo "  TESTCOMPAT=1   Outpost on RabbitMQ / SQS / Pub/Sub internal queues, backend compat suites"
	@echo "  TESTAZURE=1    separate: Azure Service Bus only (needs the emulator: LOCAL_DEV_AZURE=1 make up)"
	@echo ""
	@echo "See contributing/test.md for details."

test:
	TEST="$(TEST)" RUN="$(RUN)" TESTARGS="$(TESTARGS)" ./scripts/test.sh test

test/unit:
	TEST="$(TEST)" RUN="$(RUN)" TESTARGS="$(TESTARGS)" ./scripts/test.sh unit

test/e2e:
	RUN="$(RUN)" TESTARGS="$(TESTARGS)" ./scripts/test.sh e2e

test/full:
	TEST="$(TEST)" RUN="$(RUN)" TESTARGS="$(TESTARGS)" ./scripts/test.sh full

test/e2e/rediscluster:
	@echo "Running Redis cluster e2e tests in Docker container..."
	@if ! docker ps | grep -q test-runner; then \
		echo "Error: test-runner container not running. Run 'make up/test/rediscluster' first."; \
		exit 1; \
	fi
	@docker exec test-runner sh -c "cd /app && go test ./cmd/e2e -v -run TestE2E_Compat_RedisCluster"
	@echo "Redis cluster e2e tests completed."

test/race:
	TESTRACE=1 gotestsum --hide-summary=skipped --format-hide-empty-pkg --packages="$(TEST)" -- $(TESTARGS) -race

test/coverage:
	gotestsum --hide-summary=skipped --format-hide-empty-pkg --packages="$(TEST)" -- $(TESTARGS) -coverprofile=coverage.out

test/coverage/html:
	go tool cover -html=coverage.out

migrate:
	./build/dev/dev.sh run --rm migrate

redis/debug:
	go run cmd/redis-debug/main.go $(ARGS)

logs:
	docker logs $$(docker ps -f name=outpost-${SERVICE} --format "{{.ID}}") -f $(ARGS)

# Build Docker image for current branch with a version tag (e.g. make docker/build TAG=v0.13.3-beta).
# Produces hookdeck/outpost:<TAG>-amd64 and hookdeck/outpost:<TAG>-arm64.
# Use docker/push to push to Docker Hub: DOCKER_USER=<your-username> make docker/push TAG=v0.13.3-beta
docker/build:
	@if [ -z "$(TAG)" ]; then echo "Usage: make docker/build TAG=v0.13.3-beta"; exit 1; fi
	GORELEASER_CURRENT_TAG=$(TAG) goreleaser release -f ./build/.goreleaser.yaml --snapshot --clean --skip=sign

# Tag and push image to Docker Hub under DOCKER_USER (e.g. make docker/push DOCKER_USER=alexbouchard TAG=v0.13.3-beta).
# Requires: docker login first.
docker/push:
	@if [ -z "$(DOCKER_USER)" ] || [ -z "$(TAG)" ]; then echo "Usage: make docker/push DOCKER_USER=<your-dockerhub-username> TAG=v0.13.3-beta"; exit 1; fi
	docker tag hookdeck/outpost:$(TAG)-amd64 $(DOCKER_USER)/outpost:$(TAG)-amd64
	docker tag hookdeck/outpost:$(TAG)-arm64 $(DOCKER_USER)/outpost:$(TAG)-arm64
	docker push $(DOCKER_USER)/outpost:$(TAG)-amd64
	docker push $(DOCKER_USER)/outpost:$(TAG)-arm64
	docker manifest create $(DOCKER_USER)/outpost:$(TAG) --amend $(DOCKER_USER)/outpost:$(TAG)-amd64 --amend $(DOCKER_USER)/outpost:$(TAG)-arm64
	docker manifest push $(DOCKER_USER)/outpost:$(TAG)
	@echo "Pushed $(DOCKER_USER)/outpost:$(TAG) (amd64, arm64, and multi-arch manifest)"
