BINARY := sk6

.PHONY: build test install clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

install:
	go install .
	mkdir -p "$$(go env GOPATH)/bin"
	cp "$$(go env GOPATH)/bin/smart-k6" "$$(go env GOPATH)/bin/$(BINARY)"

clean:
	rm -f $(BINARY)
