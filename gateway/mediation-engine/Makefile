VERSION ?= 0.0.1-SNAPSHOT
DOCKER_REGISTRY ?= ghcr.io/wso2/api-platform
IMAGE_NAME ?= $(DOCKER_REGISTRY)/mediation-engine

.PHONY: build
build:
	@echo "Building Docker image $(IMAGE_NAME):$(VERSION)..."
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE_NAME):$(VERSION) .

.PHONY: build-local
build-local:
	@echo "Building Docker image locally $(IMAGE_NAME):$(VERSION)..."
	docker build -t $(IMAGE_NAME):$(VERSION) .

.PHONY: clean
clean:
	@echo "Cleaning up..."
	go clean
	rm -f mediation-engine