.PHONY: build install test clean

PREFIX ?= /usr/local/bin

build:
	go build -o amux ./cmd/amux/

install: build
	install -m 755 amux $(PREFIX)/amux

test:
	go test ./... -count=1

clean:
	rm -f amux
