TEST?=$$(go list ./...)

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
	docker-compose -f build/dev/deps/compose.yml up -d

down/deps:
	docker-compose -f build/dev/deps/compose.yml down

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

up/test:
	docker-compose -f build/test/compose.yml up -d

down/test:
	docker-compose -f build/test/compose.yml down --volumes

test:
	go test $(TEST) $(TESTARGS)

test/unit:
	go test $(TEST) $(TESTARGS) -short

test/integration:
	go test $(TEST) $(TESTARGS) -run "Integration"

test/race:
	TESTRACE=1 go test $(TEST) $(TESTARGS) -race

test/coverage:
	go test $(TEST) $(TESTARGS) -coverprofile=coverage.out

test/coverage/html:
	go tool cover -html=coverage.out

network:
	docker network create outpost

logs:
	docker logs $$(docker ps -f name=outpost-${SERVICE} --format "{{.ID}}") -f $(ARGS)
