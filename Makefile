.PHONY: build test clean local deploy image lint

build:
	go mod download
	go build -o ./build/ ./...

test:
	go test -v -race -count=1 ./services/...

# Clean up build artifacts
clean:
	go mod tidy

clean-pulumi:
	pulumi plugin rm --all --yes
	pulumi install --reinstall
