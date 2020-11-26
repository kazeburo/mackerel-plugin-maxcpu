VERSION=0.0.7
LDFLAGS=-ldflags "-w -s -X main.version=${VERSION} -X github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu.version=${VERSION}"

all: mackerel-plugin-maxcpu

.PHONY: mackerel-plugin-maxcpu

mackerel-plugin-maxcpu: main.go maxcpu/*.go
	go build $(LDFLAGS) -o mackerel-plugin-maxcpu main.go

linux: main.go maxcpu/*.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o mackerel-plugin-maxcpu main.go

check:
	go test ./...

fmt:
	go fmt ./...

tag:
	git tag v${VERSION}
	git push origin v${VERSION}
	git push origin master
