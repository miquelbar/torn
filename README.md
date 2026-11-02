# torn

A lightweight, performant service written in Go.

## Overview

`torn` is designed with simplicity, high reliability, and clean architecture principles. It provides a modular foundation with structured logging, lifecycle management, and strict configuration validation.

## Prerequisites

- [Go](https://go.dev/dl/) 1.22 or higher
- `make`

## Getting Started

### Local Setup

To set up the local development environment:

```bash
make setup
```

### Building

Compile the application binary to `bin/torn`:

```bash
make build
```

### Running

Run directly using Go:

```bash
make run
```

Or execute the built binary:

```bash
./bin/torn
```

### Testing

Run unit tests with race detection:

```bash
make test
```

Generate test coverage reports:

```bash
make test-coverage
```

### Code Quality and Linting

Run formatting and vet checks:

```bash
make lint
```

## Configuration

Configuration can be customized via environment variables:

| Variable | Type | Default | Description |
|---|---|---|---|
| `TORN_ENV` | string | `development` | Operating environment (`development`, `production`, etc.) |
| `TORN_PORT` | int | `8080` | Port for service listener |
| `TORN_SHUTDOWN_TIMEOUT` | duration | `5s` | Maximum time allowed for graceful shutdown |
| `TORN_LOG_LEVEL` | string | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`) |

## Project Structure

```txt
torn/
├── cmd/
│   └── torn/             # Application entrypoint
├── internal/
│   ├── app/              # Core application lifecycle & orchestration
│   ├── config/           # Configuration parsing & validation
│   └── logger/           # Structured logging
├── Makefile              # Build automation & verification targets
├── go.mod                # Go module dependencies
└── LICENSE               # MIT License
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feat/my-feature`)
3. Follow [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (e.g. `feat: description` or `fix: description`)
4. Verify tests and linter pass (`make check`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
