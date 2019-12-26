.PHONY: build test clean update docker run

GO = CGO_ENABLED=0 GO111MODULE=on go

MICROSERVICES=cmd/device-zigbee
.PHONY: $(MICROSERVICES)

DOCKERS=docker_device_zigbee_go
.PHONY: $(DOCKERS)

#  đọc version từ file VERSION
VERSION=$(shell cat ./VERSION)
GIT_SHA=$(shell git rev-parse HEAD)
GOFLAGS=-ldflags "-X github.com/device-zigbee.Version=$(VERSION)"

#  build project:
build: $(MICROSERVICES)
	$(GO) build ./...

cmd/device-zigbee:
	$(GO) build $(GOFLAGS) -o $@ ./cmd

test:
	$(GO) test ./... -coverprofile=coverage.out

# xóa chương trình đã build
clean:
	rm -f $(MICROSERVICES)

run:
	cd cmd && ./device-zigbee

#  để build 1 image trên máy hiện tại
docker: $(DOCKERS)

docker_device_zigbee_go:
	docker build \
		--label "git_sha=$(GIT_SHA)" \
		-t phanvanhai/docker-device-zigbee-go:$(VERSION) \
		.
