# mqcheck: validate the queue consumer on a broker. See README.md.
# Usage: make -C cmd/mqcheck up validate PROVIDER=awssqs PROFILE=quick

OUTPOST_ROOT := $(shell git rev-parse --show-toplevel)
BIN          := $(OUTPOST_ROOT)/bin/mqcheck
PROVIDER     ?= awssqs
PROFILE      ?= quick
COMPOSE      := docker compose --env-file $(OUTPOST_ROOT)/.env.test -f compose.yml

.PHONY: build up down validate doctor sweep providers cases test

build:
	cd $(OUTPOST_ROOT) && go build -o $(BIN) ./cmd/mqcheck

# Local brokers (own compose project "mqcheck", ports 44566 and 48085).
up:
	$(COMPOSE) up -d --wait

down:
	$(COMPOSE) down -v

validate: build
	$(BIN) validate --provider $(PROVIDER) --profile $(PROFILE) $(ARGS)

doctor: build
	$(BIN) doctor --provider $(PROVIDER)

sweep: build
	$(BIN) sweep --provider $(PROVIDER) $(ARGS)

providers: build
	$(BIN) providers

cases: build
	$(BIN) cases

test:
	cd $(OUTPOST_ROOT) && go test ./internal/mqcheck/... ./cmd/mqcheck/...
