# Stage 0
# Build the binaries
FROM golang:1.23-alpine
WORKDIR /app
COPY . .

# Build all binaries
RUN go build -o ./bin/outpost ./cmd/outpost/main.go && \
    go build -o ./bin/outpost-server ./cmd/outpost-server/main.go && \
    go build -o ./bin/outpost-migrate-redis ./cmd/outpost-migrate-redis/main.go

# Stage 1
# Copy binaries to a new image
FROM scratch
COPY --from=0 /app/bin/outpost /bin/outpost
COPY --from=0 /app/bin/outpost-server /bin/outpost-server
COPY --from=0 /app/bin/outpost-migrate-redis /bin/outpost-migrate-redis

# Default to running the server
ENTRYPOINT ["/bin/outpost", "serve"]
