FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /loadtest ./cmd/loadtest

FROM gcr.io/distroless/static-debian12
COPY --from=builder /loadtest /loadtest
EXPOSE 9090
ENTRYPOINT ["/loadtest"]
