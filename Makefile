TEST?=$$(go list ./...)

up:
	docker-compose -f build/dev/compose.yml --env-file .env up

down:
	docker-compose -f build/dev/compose.yml down

test:
	go test $(TEST) $(TESTARGS)

test/unit:
	go test $(TEST) $(TESTARGS) -short

test/integration:
	go test $(TEST) $(TESTARGS) -run "Integration"

test/coverage:
	go test $(TEST) $(TESTARGS) -coverprofile=coverage.out

test/coverage/html:
	go tool cover -html=coverage.out
