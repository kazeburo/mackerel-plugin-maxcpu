VERSION=0.0.1
LDFLAGS=-ldflags "-X main.Version=${VERSION}"

all: mackerel-plugin-maxcpu

.PHONY: mackerel-plugin-maxcpu

mackerel-plugin-maxcpu: main.go
	go build $(LDFLAGS) -o mackerel-plugin-maxcpu

linux: main.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o mackerel-plugin-maxcpu

check:
	go test ./...

fmt:
	go fmt ./...

tag:
	git tag v${VERSION}
	git push origin v${VERSION}
	git push origin master
	goreleaser --rm-dist
