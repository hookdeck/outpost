FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o mock-webhook .

# Use a small Alpine image for the final container
FROM alpine:3.19

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/mock-webhook .

# Expose the port the service runs on
EXPOSE 8080

# Run the service
CMD ["./mock-webhook"] 