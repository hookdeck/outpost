FROM golang:1.27-alpine AS fetch
RUN go install github.com/air-verse/air@v1.61.1
WORKDIR /app
COPY . .
ENTRYPOINT ["sh", "./build/dev/entrypoint.sh"]
