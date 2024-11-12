FROM golang:1.23-alpine AS fetch
RUN go install github.com/air-verse/air@latest
WORKDIR /app
COPY . .
CMD ["air"]
