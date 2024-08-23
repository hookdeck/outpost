FROM golang:1.23-alpine

WORKDIR /app
COPY . .

RUN go install -mod=mod github.com/githubnemo/CompileDaemon

ENTRYPOINT CompileDaemon --build="go build -o ./bin/data ./cmd/app/main.go" --command="./bin/data --service data"