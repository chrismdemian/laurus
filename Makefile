.PHONY: build run test lint clean install

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X github.com/chrismdemian/laurus/cmd.version=$(VERSION) -X github.com/chrismdemian/laurus/cmd.commit=$(COMMIT) -X github.com/chrismdemian/laurus/cmd.date=$(DATE)"

build:
	go build $(LDFLAGS) -o laurus .

run:
	go run $(LDFLAGS) . $(ARGS)

test:
	go test ./... -v

lint:
	golangci-lint run

clean:
	rm -f laurus laurus.exe
	rm -rf dist/

install:
	go install $(LDFLAGS) .
