# Stage 0
# Build the binary
FROM golang:1.23-alpine
WORKDIR /app
COPY . .

RUN go build -o ./bin/outpost ./cmd/outpost/main.go

# Stage 1
# Copy binary to a new image
FROM scratch
COPY --from=0 /app/bin/outpost /bin/outpost
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/bin/outpost"]
