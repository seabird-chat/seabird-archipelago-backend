# seabird-archipelago-backend task runner.

# Show the list of recipes
default:
    @just --list

# Build the binary into tmp/
[group('build')]
build:
    mkdir -p tmp
    CGO_ENABLED=0 go build -o tmp/seabird-archipelago-backend ./cmd/seabird-archipelago-backend

# Remove build artifacts
[group('build')]
clean:
    rm -rf tmp/

# Run all tests
[group('check')]
test:
    CGO_ENABLED=0 go test ./...

# Run linting checks
[group('check')]
lint:
    golangci-lint run

# Format all code (pass --ci to check only)
[group('check')]
fmt *args:
    treefmt {{ args }}

# Run CI's checks before committing
[group('check')]
check: (fmt "--ci") lint test

# Run the backend from source; reads .env if present
[group('dev')]
run *args:
    go run ./cmd/seabird-archipelago-backend {{ args }}
