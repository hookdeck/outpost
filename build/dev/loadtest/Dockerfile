FROM golang:1.23-alpine
RUN go install github.com/air-verse/air@v1.61.1
WORKDIR /app
COPY loadtest-app/ .
ENTRYPOINT ["air", "--build.cmd", "go build -o bin/loadtest ./cmd/loadtest", "--build.bin", "./bin/loadtest", "--build.include_ext", "go"]
