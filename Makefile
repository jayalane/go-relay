all: proxy uproxy

.PHONY: proto
proto:
	make -C udpProxy all

uproxy: proto
	cd uproxy && go fmt && golint && go vet && go build && golangci-lint run . && go test -v -v -race

proxy: proto
	go fmt && golint && go vet && go build && go test -v -v -race && go build
#	go fmt && golint && go vet && go build && golangci-lint run . && go test -v -v -race
