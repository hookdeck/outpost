# Stage 0
# Build the binary
FROM golang:1.23-alpine
WORKDIR /app
COPY . .

RUN go build -o ./bin/eventkit ./cmd/app/main.go

# Stage 1
# Copy binary to a new image
FROM scratch
COPY --from=0 /app/bin/eventkit /bin/eventkit
ENTRYPOINT ["/bin/eventkit"]
