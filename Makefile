
GO_CMD = gofumpt -w . && go fmt ./... && go generate ./... && golint ./... && go vet ./... && govulncheck ./... && staticcheck ./... && go build ./... && golangci-lint run --timeout 3m --enable-all --disable gomnd,lll,rowserrcheck,sqlclosecheck,wastedassign,wrapcheck,gomoddirectives,testpackage,gochecknoglobals,paralleltest,exhaustruct,varnamelen,forbidigo,funlen,ireturn,depguard,nolintlint -e .*G114.* --out-format line-number --path-prefix `pwd` ./...

GO_FILES = $(shell find ./ -name .git -prune -o -name \*.go )
all: all_mod ${GO_FILES}
	$(GO_CMD)

all_mod:
	go mod download

tidy: 
	go mod tidy

test:
	go test ./... -v -v -race -coverprofile fmtcoverage.html
	gotestsum  --junitfile junit.xml ./...
	cat junit.xml

.PHONY: tidy proto all_mod test

