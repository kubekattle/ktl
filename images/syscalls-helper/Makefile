IMAGE ?= ghcr.io/avkcode/ktl/syscalls-helper:latest
PLATFORM ?= linux/amd64
BUILD_ARGS ?=

.PHONY: build push

build:
	docker build --platform $(PLATFORM) -t $(IMAGE) $(BUILD_ARGS) .

push: build
	docker push $(IMAGE)
