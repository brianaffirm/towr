.PHONY: build install test clean

build:
	go build -o towr ./cmd/towr/

install:
	go install ./cmd/towr/

test:
	go test ./... -count=1

clean:
	rm -f towr
