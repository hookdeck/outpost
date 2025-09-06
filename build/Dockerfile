# Stage 0
# Build the binary
FROM golang:1.23-alpine
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN go build -o ./bin/outpost ./cmd/outpost/main.go

# Stage 1
# Copy binary to a new image
FROM scratch
COPY --from=0 /app/bin/outpost /bin/outpost
ENTRYPOINT ["/bin/outpost"]
