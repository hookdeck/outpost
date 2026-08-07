#!/bin/bash

# Default configuration
PORT=8080
MODE="direct"
BUILD=false
DOCKER_IMAGE="mock-webhook:latest"

# Help function
show_help() {
  echo "Usage: $0 [options]"
  echo ""
  echo "Options:"
  echo "  --port=PORT        HTTP port to listen on (default: $PORT)"
  echo "  --mode=MODE        Run mode: direct, binary, docker (default: $MODE)"
  echo "  --build            Build binary before running (only for binary mode)"
  echo "  --docker-image=IMG Docker image to use (default: $DOCKER_IMAGE)"
  echo "  --help             Show this help message"
  echo ""
  echo "Example:"
  echo "  $0 --port=9090 --mode=binary --build"
  echo "  $0 --mode=docker --docker-image=hookdeck/mock-webhook:v1.0.0"
}

# Parse arguments
for arg in "$@"; do
  case $arg in
    --port=*)
      PORT="${arg#*=}"
      shift
      ;;
    --mode=*)
      MODE="${arg#*=}"
      shift
      ;;
    --build)
      BUILD=true
      shift
      ;;
    --docker-image=*)
      DOCKER_IMAGE="${arg#*=}"
      shift
      ;;
    --help)
      show_help
      exit 0
      ;;
    *)
      echo "Unknown option: $arg"
      show_help
      exit 1
      ;;
  esac
done

# Validate mode
if [[ "$MODE" != "direct" && "$MODE" != "binary" && "$MODE" != "docker" ]]; then
  echo "Error: Invalid mode '$MODE'. Must be one of: direct, binary, docker"
  exit 1
fi

# Get the project root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Go to project root
cd "$PROJECT_ROOT" || { echo "Failed to change to project directory"; exit 1; }

echo "Starting webhook mock service in $MODE mode on port $PORT"

case $MODE in
  "direct")
    # Run directly with Go
    echo "Running with 'go run'"
    PORT=$PORT go run main.go
    ;;
    
  "binary")
    # Build if requested
    if [[ "$BUILD" == true ]]; then
      echo "Building binary..."
      go build -o mock-webhook .
      if [[ $? -ne 0 ]]; then
        echo "Failed to build binary"
        exit 1
      fi
    fi
    
    # Check if binary exists
    if [[ ! -f "./mock-webhook" ]]; then
      echo "Binary not found. Run with --build flag or build manually first."
      exit 1
    fi
    
    # Run the binary
    echo "Running binary"
    PORT=$PORT ./mock-webhook
    ;;
    
  "docker")
    # Check if Docker is installed
    if ! command -v docker &> /dev/null; then
      echo "Docker is not installed. Please install Docker first."
      exit 1
    fi
    
    # Check if image exists
    if ! docker image inspect "$DOCKER_IMAGE" &> /dev/null; then
      echo "Docker image $DOCKER_IMAGE not found."
      echo "Build it first with: ./scripts/build.sh --name=${DOCKER_IMAGE%%:*} --tag=${DOCKER_IMAGE##*:}"
      exit 1
    fi
    
    # Run Docker container
    echo "Running in Docker container: $DOCKER_IMAGE"
    docker run --rm -p "$PORT:8080" -e PORT=8080 "$DOCKER_IMAGE"
    ;;
esac 