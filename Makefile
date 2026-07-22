IMAGE_NAME ?= troleum-app:local
CONTAINER_NAME ?= troleum_app
HOST_PORT ?= 8080
CONTAINER_PORT ?= 8080
LOCAL_ENV_FILE ?= .env
LOCAL_JSON_DIR ?= $(CURDIR)/json
LOCAL_STATIC_DIR ?= $(CURDIR)/static

REMOTE_IMAGE_NAME ?= troleum-app:runtime
REMOTE_IMAGE_TAR ?= troleum_runtime.tar
REMOTE_HOST ?= golf2
REMOTE_PLATFORM ?= linux/amd64
REMOTE_ENGINE ?= podman
REMOTE_UID ?= 1001:1001
REMOTE_BASE_DIR ?= /home/deploy/troleum
REMOTE_JSON_DIR ?= $(REMOTE_BASE_DIR)/json
REMOTE_ENV_FILE_LOCAL ?= .env.prod
REMOTE_ENV_FILE_REMOTE ?= .env
REMOTE_CONTAINER_NAME ?= troleum_app
REMOTE_HOST_PORT ?= 8080
REMOTE_CONTAINER_PORT ?= 8080
REMOTE_EXTRA_RUN_ARGS ?=

GOAMD64 ?= v3
ASSET_VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)-$(shell date +%Y%m%d%H%M%S)

.PHONY: help build run stop restart logs logs-app open clean test rebuildrun \
	build-remote-image save-image send-image send-prod-env run-remote deploy-to-production delete-local-image-tar remote-logs

help:
	@echo "Available targets:"
	@echo "  make build    - docker build the local image"
	@echo "  make run      - build and run the local app with .env loaded by docker run"
	@echo "  make stop     - stop and remove the local container"
	@echo "  make restart  - stop, rebuild, and run again"
	@echo "  make logs     - follow local app logs"
	@echo "  make open     - open the site in your browser"
	@echo "  make clean    - stop the app and remove local images/tars"
	@echo "  make test     - run Go tests"
	@echo "  make build-remote-image   - build the production runtime image for $(REMOTE_PLATFORM)"
	@echo "  make save-image           - save the production image to a tar file"
	@echo "  make send-image           - upload the image tar to the remote host"
	@echo "  make send-prod-env        - upload $(REMOTE_ENV_FILE_LOCAL) to the remote host"
	@echo "  make run-remote           - load and run the container on the remote host with podman"
	@echo "  make deploy-to-production - test, build, ship, and run on the remote host"
	@echo "  make remote-logs          - follow remote container logs"
	@echo ""
	@echo "Build tuning vars:"
	@echo "  GOAMD64=$(GOAMD64)"
	@echo "  ASSET_VERSION=$(ASSET_VERSION)"
	@echo "  LOCAL_ENV_FILE=$(LOCAL_ENV_FILE)"
	@echo "  REMOTE_EXTRA_RUN_ARGS=$(REMOTE_EXTRA_RUN_ARGS)"

build:
	docker build \
		--build-arg ASSET_VERSION=$(ASSET_VERSION) \
		--build-arg GOAMD64=$(GOAMD64) \
		--target runtime -t $(IMAGE_NAME) .

run: stop
	@test -f $(LOCAL_ENV_FILE) || { echo "missing $(LOCAL_ENV_FILE)"; exit 1; }
	mkdir -p $(LOCAL_JSON_DIR)
	$(MAKE) build
	docker run -d --name $(CONTAINER_NAME) --restart unless-stopped \
		--env-file $(LOCAL_ENV_FILE) \
		-p $(HOST_PORT):$(CONTAINER_PORT) \
		-v $(LOCAL_JSON_DIR):/app/json \
		-v $(LOCAL_STATIC_DIR):/app/static \
		$(IMAGE_NAME)
	@echo "Troleum running at http://localhost:$(HOST_PORT)"

stop:
	docker rm -f $(CONTAINER_NAME) >/dev/null 2>&1 || true

restart: stop run

logs:
	docker logs -f $(CONTAINER_NAME)

logs-app: logs

open:
	open http://localhost:$(HOST_PORT)

test:
	go test ./app -count=1

clean: stop
	docker rmi -f $(IMAGE_NAME) >/dev/null 2>&1 || true
	rm -f $(REMOTE_IMAGE_TAR)

rebuildrun: test stop clean run logs

build-remote-image:
	docker buildx build --platform $(REMOTE_PLATFORM) \
		--build-arg ASSET_VERSION=$(ASSET_VERSION) \
		--build-arg GOAMD64=$(GOAMD64) \
		--target runtime --load -t $(REMOTE_IMAGE_NAME) .

save-image: build-remote-image
	docker save $(REMOTE_IMAGE_NAME) -o $(REMOTE_IMAGE_TAR)

send-image:
	ssh $(REMOTE_HOST) "mkdir -p $(REMOTE_BASE_DIR) $(REMOTE_JSON_DIR) && chmod 700 $(REMOTE_BASE_DIR) $(REMOTE_JSON_DIR)"
	scp $(REMOTE_IMAGE_TAR) $(REMOTE_HOST):$(REMOTE_BASE_DIR)/$(REMOTE_IMAGE_TAR)

send-prod-env:
	ssh $(REMOTE_HOST) "mkdir -p $(REMOTE_BASE_DIR) && chmod 700 $(REMOTE_BASE_DIR)"
	scp $(REMOTE_ENV_FILE_LOCAL) $(REMOTE_HOST):$(REMOTE_BASE_DIR)/$(REMOTE_ENV_FILE_REMOTE)
	ssh $(REMOTE_HOST) "chmod 600 $(REMOTE_BASE_DIR)/$(REMOTE_ENV_FILE_REMOTE)"

run-remote:
	ssh $(REMOTE_HOST) "$(REMOTE_ENGINE) load -i $(REMOTE_BASE_DIR)/$(REMOTE_IMAGE_TAR)"
	ssh $(REMOTE_HOST) "$(REMOTE_ENGINE) rm -f $(REMOTE_CONTAINER_NAME) >/dev/null 2>&1 || true"
	ssh $(REMOTE_HOST) "$(REMOTE_ENGINE) run -d --restart unless-stopped --platform $(REMOTE_PLATFORM) --user $(REMOTE_UID) -p $(REMOTE_HOST_PORT):$(REMOTE_CONTAINER_PORT) -v $(REMOTE_JSON_DIR):/app/json:Z --env-file $(REMOTE_BASE_DIR)/$(REMOTE_ENV_FILE_REMOTE) $(REMOTE_EXTRA_RUN_ARGS) --name $(REMOTE_CONTAINER_NAME) $(REMOTE_IMAGE_NAME)"
	ssh $(REMOTE_HOST) "rm -f $(REMOTE_BASE_DIR)/$(REMOTE_IMAGE_TAR)"

deploy-to-production: test save-image send-image send-prod-env run-remote delete-local-image-tar

delete-local-image-tar:
	rm -f $(REMOTE_IMAGE_TAR)

remote-logs:
	ssh $(REMOTE_HOST) "$(REMOTE_ENGINE) logs -f $(REMOTE_CONTAINER_NAME)"