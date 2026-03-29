VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)

LDFLAGS := -s -w -buildid= -X main.version=$(VERSION) -X main.commit=$(COMMIT)
FLAGS   := -trimpath -buildvcs=false -ldflags="$(LDFLAGS)"

.PHONY: build test vet race bench clean

build:
	CGO_ENABLED=0 go build $(FLAGS) -o tonmon ./cmd/tonmon/

test:
	go test ./... -count=1

vet:
	go vet ./...

race:
	go test -race ./... -count=1

bench:
	go test ./internal/dedup/... -bench=. -benchmem -count=3
	go test ./internal/webhook/... -bench=. -benchmem -count=3

clean:
	rm -f tonmon
	rm -rf ./data/
