FROM golang:1.23-alpine

WORKDIR /app
COPY . .

RUN go install -mod=mod github.com/githubnemo/CompileDaemon

ENTRYPOINT CompileDaemon --build="go build -o ./bin/api ./cmd/eventkit/main.go" --command="./bin/api --service api"