PACKAGE = github.com/vechain/thor

GIT_COMMIT = $(shell git --no-pager log --pretty="%h" -n 1)
GIT_TAG = $(shell git tag -l --points-at HEAD)
THOR_VERSION = $(shell cat cmd/thor/VERSION)
DISCO_VERSION = $(shell cat cmd/disco/VERSION)

PACKAGES = `go list ./... | grep -v '/vendor/'`

REQUIRED_GO_MAJOR = 1
REQUIRED_GO_MINOR = 16
MAJOR = $(shell go version | cut -d' ' -f3 | cut -b 3- | cut -d. -f1)
MINOR = $(shell go version | cut -d' ' -f3 | cut -b 3- | cut -d. -f2)
export GO111MODULE=on

FUZZTIME=5s

.PHONY: thor disco all clean test

thor:| go_version_check
	@echo "building $@..."
	@go build -v -o $(CURDIR)/bin/$@ -ldflags "-X main.version=$(THOR_VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.gitTag=$(GIT_TAG)" ./cmd/thor
	@echo "done. executable created at 'bin/$@'"

disco:| go_version_check
	@echo "building $@..."
	@go build -v -o $(CURDIR)/bin/$@ -ldflags "-X main.version=$(DISCO_VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.gitTag=$(GIT_TAG)" ./cmd/disco
	@echo "done. executable created at 'bin/$@'"

dep:| go_version_check
	@go mod download

go_version_check:
	@if test $(MAJOR) -lt $(REQUIRED_GO_MAJOR); then \
		echo "Go $(REQUIRED_GO_MAJOR).$(REQUIRED_GO_MINOR) or higher required"; \
		exit 1; \
	else \
		if test $(MAJOR) -eq  $(REQUIRED_GO_MAJOR) -a $(MINOR) -lt $(REQUIRED_GO_MINOR); then \
			echo "Go $(REQUIRED_GO_MAJOR).$(REQUIRED_GO_MINOR) or higher required"; \
			exit 1; \
		fi \
	fi

all: thor disco

clean:
	-rm -rf \
$(CURDIR)/bin/thor \
$(CURDIR)/bin/disco 

test:| go_version_check
	@go test -cover $(PACKAGES)

fuzz:| go_version_check
	@go test -fuzz=FuzzBitmap -fuzztime=$(FUZZTIME) github.com/vechain/thor/vm/
	@go test -fuzz=FuzzContract -fuzztime=$(FUZZTIME) github.com/vechain/thor/vm/
	@go test -fuzz=FuzzReserved -fuzztime=$(FUZZTIME) github.com/vechain/thor/tx/
	@go test -fuzz=FuzzTransaction -fuzztime=$(FUZZTIME) github.com/vechain/thor/tx/
	@go test -fuzz=FuzzParseNode -fuzztime=$(FUZZTIME) github.com/vechain/thor/p2psrv/discv5/
	@go test -fuzz=FuzzPacket -fuzztime=$(FUZZTIME) github.com/vechain/thor/p2psrv/discv5/
	@go test -fuzz=FuzzBlock -fuzztime=$(FUZZTIME) github.com/vechain/thor/block/
	@go test -fuzz=FuzzHeader -fuzztime=$(FUZZTIME) github.com/vechain/thor/block/
