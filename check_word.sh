#!/bin/bash
# Load environment variables from .env
if [ -f .env ]; then
  export $(grep -v '^#' .env | xargs)
fi

# Run the Go word check command in the service-data directory
cd service-data && go run cmd/check/main.go "$@"
