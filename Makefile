build:
	go build ./...
test:
	go test ./... -v -count=1
vet:
	go vet ./...
fmt:
	gofmt -w .
lint: vet
	@if command -v staticcheck > /dev/null 2>&1; then staticcheck ./...; else echo "staticcheck not installed"; fi
clean:
	go clean -testcache
check: fmt vet test
