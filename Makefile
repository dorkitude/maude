BINARY := maude
PREFIX ?= /usr/local

.PHONY: build
build:
	go build -o $(BINARY) ./cmd/maude

.PHONY: test
test:
	go test ./...

.PHONY: fmt
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './state/*')

.PHONY: install
install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BINARY) $(PREFIX)/bin/$(BINARY)

.PHONY: clean
clean:
	rm -f $(BINARY) coverage.out
