FROM golang:1.23-alpine

WORKDIR /app
COPY . .

RUN go install -mod=mod github.com/githubnemo/CompileDaemon

ENTRYPOINT CompileDaemon --build="go build -o ./bin/log ./cmd/eventkit/main.go" --command="./bin/log --service log"