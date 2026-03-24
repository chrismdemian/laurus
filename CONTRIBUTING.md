# Contributing to Laurus

Thanks for your interest in contributing!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/your-username/laurus.git`
3. Install Go 1.26+: [go.dev/dl](https://go.dev/dl/)
4. Install dependencies: `go mod download`
5. Create a branch: `git checkout -b feature/your-feature`

## Development

```bash
# Build
go build -o laurus .

# Run
go run . next

# Test
go test ./...

# Lint (requires golangci-lint)
golangci-lint run
```

## Pull Requests

- Keep PRs focused on a single change
- Include a description of what and why
- Add tests for new functionality
- Run `go test ./...` before submitting

## Reporting Issues

- Use GitHub Issues
- Include your OS and Go version
- Include your Canvas instance URL (not your token)
- Include steps to reproduce

## Code Style

- Go throughout — follow standard Go conventions
- Use `internal/` for non-exported packages
- Keep it simple — minimal abstractions
- Follow existing patterns in the codebase
- Run `gofmt` and `go vet` before committing

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
