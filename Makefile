# RePanel build targets

VERSION ?= 0.1.0
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: all web build linux dev-backend clean

all: build

web:
	cd web && npm install && npm run build

build: web
	mkdir -p dist
	go build -ldflags "$(LDFLAGS)" -o dist/repanel ./cmd/repanel

# cross-compile release binaries (run on any OS)
linux: web
	mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/repanel-linux-amd64 ./cmd/repanel
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/repanel-linux-arm64 ./cmd/repanel

dev-backend:
	go run ./cmd/repanel -config dev.conf

clean:
	rm -rf dist web/dist
