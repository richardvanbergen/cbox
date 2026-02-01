.PHONY: build install clean hostcmd-client

HOSTCMD_CLIENT_DIR = internal/docker

hostcmd-client:
	GOOS=linux GOARCH=amd64 go build -o $(HOSTCMD_CLIENT_DIR)/hostcmd-client-linux-amd64 ./cmd/cbox-host-cmd-client
	GOOS=linux GOARCH=arm64 go build -o $(HOSTCMD_CLIENT_DIR)/hostcmd-client-linux-arm64 ./cmd/cbox-host-cmd-client

build: hostcmd-client
	go build -o bin/cbox ./cmd/cbox

install: hostcmd-client
	go install ./cmd/cbox

clean:
	rm -rf bin/
	rm -f $(HOSTCMD_CLIENT_DIR)/hostcmd-client-linux-*
