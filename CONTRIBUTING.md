# Contributing to HarborBuddy

Thank you for your interest in contributing to HarborBuddy! This document provides guidelines for contributing to the project.

## Development Setup

### Prerequisites

- Go 1.23 or later
- Docker (for building container images and testing)
- Make (optional, for convenience commands)

### Getting Started

1. Fork and clone the repository:

```bash
git clone https://github.com/MikeO7/HarborBuddy.git
cd HarborBuddy
```

2. Install dependencies:

```bash
go mod download
```

3. Build the project:

```bash
make build
# or
go build -o harborbuddy ./cmd/harborbuddy
```

4. Run tests:

```bash
make test
# or
go test ./...
```

## Project Structure

```
├── cmd/
│   └── harborbuddy/        # Main application entry point
├── internal/
│   ├── cleanup/            # Image cleanup logic
│   ├── config/             # Configuration loading and merging
│   ├── docker/             # Docker API client wrapper
│   ├── scheduler/          # Main scheduler loop
│   └── updater/            # Container update logic
├── pkg/
│   └── log/                # Logging utilities
├── examples/               # Example configurations
│   ├── docker-compose.yml
│   └── harborbuddy.yml
└── Dockerfile              # Multi-stage Docker build
```

## Development Workflow

### Making Changes

1. Create a new branch:

```bash
git checkout -b feature/your-feature-name
```

2. Make your changes and ensure code is formatted:

```bash
make fmt
# or
go fmt ./...
```

3. Run tests:

```bash
make test
```

4. Run linter (if available):

```bash
make lint
```

5. Build and test locally:

```bash
make run-dry
```

### Commit Messages

Use clear and descriptive commit messages:

- Use present tense ("Add feature" not "Added feature")
- Use imperative mood ("Move cursor to..." not "Moves cursor to...")
- Limit first line to 72 characters
- Reference issues and pull requests liberally

Examples:
- `Add support for remote Docker hosts`
- `Fix container recreation when ports are exposed`
- `Update README with new configuration options`

### Testing

- Write tests for new features
- Ensure existing tests pass
- Add integration tests for complex scenarios
- Test with real Docker containers when possible

Example test structure:

```go
func TestYourFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"case 1", "input1", "expected1"},
        {"case 2", "input2", "expected2"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := YourFunction(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

## Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Keep functions focused and small
- Add comments for exported functions
- Use meaningful variable names

## Pull Request Process

1. Update the README.md with details of changes if needed
2. Update the CHANGELOG.md (if exists) with notable changes
3. Ensure all tests pass and code is formatted
4. Update documentation for new features
5. The PR will be merged once you have approval from maintainers

### PR Checklist

- [ ] Code builds successfully
- [ ] Tests pass
- [ ] Code is formatted (`go fmt`)
- [ ] New tests added for new features
- [ ] Documentation updated
- [ ] No linter errors
- [ ] Commit messages are clear

## Adding New Features

When adding new features, consider:

1. **Backward compatibility**: Don't break existing configurations
2. **Documentation**: Update README and examples
3. **Testing**: Add comprehensive tests
4. **Logging**: Add appropriate log messages
5. **Error handling**: Handle errors gracefully

### Feature Requests

For major features, please open an issue first to discuss the approach before implementing.

## Bug Reports

When reporting bugs, please include:

1. HarborBuddy version (`harborbuddy --version`)
2. Docker version
3. Operating system and version
4. Configuration file (sanitized)
5. Relevant log output
6. Steps to reproduce
7. Expected behavior
8. Actual behavior

## Development Tips

### Testing with Docker Compose

Create a test docker-compose.yml:

```yaml
services:
  harborbuddy:
    build: .
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./examples/harborbuddy.yml:/config/harborbuddy.yml:ro
    environment:
      HARBORBUDDY_LOG_LEVEL: debug
      HARBORBUDDY_DRY_RUN: "true"
    labels:
      com.harborbuddy.autoupdate: "false"

  test-nginx:
    image: nginx:latest
    ports:
      - "8080:80"
```

### Debugging

Enable debug logging:

```bash
./harborbuddy --log-level debug --dry-run --once
```

### Local Development

Run in dry-run mode to test without affecting containers:

```bash
./harborbuddy --dry-run --once --log-level debug
```

## Questions?

If you have questions, please:

1. Check existing issues and discussions
2. Read the documentation thoroughly
3. Open a new issue with your question

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.

## Code of Conduct

- Be respectful and inclusive
- Welcome newcomers
- Focus on constructive feedback
- Assume good intentions

Thank you for contributing to HarborBuddy! 🚢

