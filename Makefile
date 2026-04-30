.PHONY: test
test:
	staticcheck ./...
	go vet ./...
	go test -trimpath -short ./...

.PHONY: coverage
coverage:
	go test -trimpath -short -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

version ?= minor

.PHONY: release
release: test
	go run github.com/kevinburke/bump_version@latest --tag-prefix=v $(version) version.go

.PHONY: force
force: ;

.PHONY: fmt
fmt:
	go fmt ./...
