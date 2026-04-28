.PHONY: build
build:
	go build -o bin/whodis cmd/whodis/main.go

.PHONY: test
test:
	go test ./...

