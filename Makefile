PROJECT := unobin-library-aws
DIR_ROOT := $(realpath $(CURDIR))
DIR_OUT  := _output
UID := $(shell id -u)
GID := $(shell id -g)

CTR_IMAGE_GO         := ghcr.io/cloudboss/docker.io/library/golang:1.26.2-alpine3.23
CTR_IMAGE_LINT       := ghcr.io/cloudboss/docker.io/golangci/golangci-lint:v2.11.4-alpine
CTR_IMAGE_LOCALSTACK := ghcr.io/cloudboss/docker.io/localstack/localstack:4.14.0
CTR_IMAGE_MINISTACK  := ghcr.io/cloudboss/docker.io/ministackorg/ministack:1.3.66

NETWORK         := $(PROJECT)-it
LOCALSTACK_NAME := $(PROJECT)-localstack
MINISTACK_NAME  := $(PROJECT)-ministack

UNOBIN_VERSION := $(shell awk '/github.com[/]cloudboss[/]unobin v/{print $$2}' go.mod)
DOCGEN ?= go run github.com/cloudboss/cloudboss-docs/unobin/cmd/docgen@v0.2.1

.DEFAULT_GOAL := help

.PHONY: help docs lint emulators-down emulators-up \
	test test-integration-live test-integration-emulator

help:
	@echo 'Targets:'
	@echo '  emulators-down               Stop the emulator containers and remove the network.'
	@echo '  emulators-up                 Start the pinned LocalStack and ministack containers.'
	@echo '  docs                         Generate the reference manual.'
	@echo '  test                         Run unit tests on the host.'
	@echo '  test-integration-live        Run integration tests against real AWS (UNOBIN_AWS_LIVE=1).'
	@echo '  test-integration-emulator    Run integration tests against the emulators.'

$(DIR_OUT):
	@mkdir -p $(@)

$(DIR_OUT)/%/: $(DIR_OUT)
	@mkdir -p $(DIR_OUT)/$(*)

$(DIR_OUT)/.command-%:
	@[ -f $(DIR_OUT)/.command-$* ] || { \
		which $(*) >/dev/null 2>&1 && \
		mkdir -p $(DIR_OUT) && touch $(DIR_OUT)/.command-$(*) || \
		(echo "command $(*) is required"; exit 1); \
	}

HAS_COMMAND_CURL := $(DIR_OUT)/.command-curl

docs:
	@$(DOCGEN) --root $(DIR_ROOT) --collection docs/reference-libraries.json --out docs/reference

test:
	@go test -v ./...

lint:
	@docker run --rm \
		-v $(DIR_ROOT):/code:z \
		-v $(DIR_ROOT)/../unobin:/unobin:z \
		-w /code $(CTR_IMAGE_LINT) golangci-lint run -v ./...

emulators-up: | $(HAS_COMMAND_CURL)
	@docker network inspect $(NETWORK) >/dev/null 2>&1 \
		|| docker network create $(NETWORK) >/dev/null
	@docker run -d --rm \
		--name $(LOCALSTACK_NAME) \
		--network $(NETWORK) \
		-p 4566:4566 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		--security-opt label=type:container_runtime_t \
		-e DEBUG=1 \
		$(CTR_IMAGE_LOCALSTACK) >/dev/null
	@docker run -d --rm \
		--name $(MINISTACK_NAME) \
		--network $(NETWORK) \
		-p 4567:4566 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		--security-opt label=type:container_runtime_t \
		-e DEBUG=1 \
		$(CTR_IMAGE_MINISTACK) >/dev/null
	@i=0; while [ $${i} -lt 30 ]; do \
		if curl -fsS http://localhost:4566/_localstack/health >/dev/null 2>&1 \
			&& curl -fsS http://localhost:4567/_localstack/health >/dev/null 2>&1; then \
			exit 0; \
		fi; \
		i=$$((i + 1)); \
		sleep 1; \
	done; \
	echo 'emulators did not come up in 30s' >&2; \
	docker logs $(LOCALSTACK_NAME); \
	docker logs $(MINISTACK_NAME); \
	exit 1

emulators-down:
	@docker rm -f $(LOCALSTACK_NAME) >/dev/null 2>&1 || true
	@docker rm -f $(MINISTACK_NAME) >/dev/null 2>&1 || true
	@docker network rm $(NETWORK) >/dev/null 2>&1 || true

# Live tier passes through the AWS_* envs from the user's shell
# so existing credentials and region are honored.
test-integration-live: | $(DIR_OUT)/xdg-cache/
	@docker run --rm \
		-v $(DIR_ROOT):/code:z \
		-v $(DIR_ROOT)/$(DIR_OUT)/xdg-cache:/awshome/.cache:z \
		-v $(HOME)/.aws:/awshome/.aws:ro,z \
		-u $(UID):$(GID) \
		-w /code \
		-e HOME=/awshome \
		-e GOPATH=/code/$(DIR_OUT)/go \
		-e GOCACHE=/code/$(DIR_OUT)/gocache \
		-e UNOBIN_VERSION=$(UNOBIN_VERSION) \
		-e SCENARIO \
		-e UNOBIN_AWS_LIVE \
		-e AWS_ACCESS_KEY_ID \
		-e AWS_SECRET_ACCESS_KEY \
		-e AWS_SESSION_TOKEN \
		-e AWS_PROFILE \
		-e AWS_REGION \
		-e AWS_DEFAULT_REGION \
		$(CTR_IMAGE_GO) sh -c './tests/integration/run.sh live'

test-integration-emulator: emulators-up | $(DIR_OUT)/xdg-cache/
	@docker run --rm \
		--network $(NETWORK) \
		-v $(DIR_ROOT):/code:z \
		-v $(DIR_ROOT)/$(DIR_OUT)/xdg-cache:/.cache:z \
		-u $(UID):$(GID) \
		-w /code \
		-e GOPATH=/code/$(DIR_OUT)/go \
		-e GOCACHE=/code/$(DIR_OUT)/gocache \
		-e UNOBIN_VERSION=$(UNOBIN_VERSION) \
		-e SCENARIO \
		-e LOCALSTACK_ENDPOINT=http://$(LOCALSTACK_NAME):4566 \
		-e MINISTACK_ENDPOINT=http://$(MINISTACK_NAME):4566 \
		$(CTR_IMAGE_GO) sh -c './tests/integration/run.sh emulator' \
		; RC=$${?}; \
		$(MAKE) emulators-down; \
		exit $${RC}
