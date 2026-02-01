.PHONY: build install clean

build:
	go build -o bin/cbox ./cmd/cbox

install:
	go install ./cmd/cbox

clean:
	rm -rf bin/
