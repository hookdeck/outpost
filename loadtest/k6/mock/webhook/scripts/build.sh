#!/bin/bash

# Default image name
DEFAULT_IMAGE_NAME="mock-webhook"
DEFAULT_IMAGE_TAG="latest"

# Help function
show_help() {
  echo "Usage: $0 [options]"
  echo ""
  echo "Options:"
  echo "  --name=NAME       Docker image name (default: $DEFAULT_IMAGE_NAME)"
  echo "  --tag=TAG         Docker image tag (default: $DEFAULT_IMAGE_TAG)"
  echo "  --push            Push the image to container registry"
  echo "  --help            Show this help message"
  echo ""
  echo "Example:"
  echo "  $0 --name=my-webhook-image --tag=v1.0.0"
}

# Parse arguments
IMAGE_NAME=$DEFAULT_IMAGE_NAME
IMAGE_TAG=$DEFAULT_IMAGE_TAG
PUSH_IMAGE=false

for arg in "$@"; do
  case $arg in
    --name=*)
      IMAGE_NAME="${arg#*=}"
      shift
      ;;
    --tag=*)
      IMAGE_TAG="${arg#*=}"
      shift
      ;;
    --push)
      PUSH_IMAGE=true
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

echo "Building Docker image: $IMAGE_NAME:$IMAGE_TAG"

# Build the Docker image
docker build -t "$IMAGE_NAME:$IMAGE_TAG" .

# Check if build was successful
if [ $? -eq 0 ]; then
  echo "✅ Docker image built successfully: $IMAGE_NAME:$IMAGE_TAG"
  
  # Push the image if requested
  if [ "$PUSH_IMAGE" = true ]; then
    echo "Pushing image to container registry..."
    docker push "$IMAGE_NAME:$IMAGE_TAG"
    
    if [ $? -eq 0 ]; then
      echo "✅ Image pushed successfully"
    else
      echo "❌ Failed to push image"
      exit 1
    fi
  fi
  
  echo ""
  echo "To run the container:"
  echo "docker run -p 8080:8080 $IMAGE_NAME:$IMAGE_TAG"
else
  echo "❌ Docker build failed"
  exit 1
fi 