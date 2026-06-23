.PHONY: all build test check

all: build test check

build:
	go build -o bin/whodis cmd/whodis/main.go

test:
	go test ./...

check:
	go run honnef.co/go/tools/cmd/staticcheck ./...
	go run golang.org/x/vuln/cmd/govulncheck ./...
