VERSION=0.0.11
LDFLAGS=-ldflags "-w -s -X main.version=${VERSION} -X github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu.version=${VERSION}"

all: mackerel-plugin-maxcpu

.PHONY: mackerel-plugin-maxcpu

mackerel-plugin-maxcpu: main.go maxcpu/*.go
	go build $(LDFLAGS) -o mackerel-plugin-maxcpu main.go

linux: main.go maxcpu/*.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o mackerel-plugin-maxcpu main.go

linux-check: mackerel-plugin-maxcpu
	@bash -xc ' \
	set -e; \
	tmpfile=$$(mktemp tmpfile.XXXXXX); \
	trap "rm -f $$tmpfile" EXIT; \
	rm -f $$tmpfile; \
	./mackerel-plugin-maxcpu -s $$tmpfile; \
	sleep 5; \
	./mackerel-plugin-maxcpu -s $$tmpfile | grep maxcpu; \
	sleep 5; \
	lines=$$(./mackerel-plugin-maxcpu -s $$tmpfile | wc -l); \
	if [ "$$lines" -ne 5 ]; then \
		echo "Expected 5 lines, got $$lines"; \
		exit 1; \
	fi; \
	pkill -f $$tmpfile'

check:
	go test ./...

fmt:
	go fmt ./...

