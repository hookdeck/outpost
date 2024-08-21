FROM golang:1.23-alpine

WORKDIR /app
COPY . .

RUN go install -mod=mod github.com/githubnemo/CompileDaemon

ENTRYPOINT CompileDaemon --build="go build -o ./cmd/api/main ./cmd/api/main.go" --command=./cmd/api/main