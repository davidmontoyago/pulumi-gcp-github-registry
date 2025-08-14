.PHONY: build test clean local deploy image lint

build:
	go mod download
	go build -o ./build/ ./...

test:
	go test -v -race -count=1 -coverprofile=coverage.out ./...

# Clean up build artifacts
clean:
	go mod tidy

clean-pulumi:
	pulumi plugin rm --all --yes
	pulumi install --reinstall

lint:
	docker run --rm -v $$(pwd):/app \
		-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
		-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
		-w /app golangci/golangci-lint:v2.1.6 \
		golangci-lint run --fix --verbose --output.text.colors
