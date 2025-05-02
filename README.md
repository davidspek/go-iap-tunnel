# go-iap-tunnel

A Go package and CLI tool for creating secure tunnels to Google Cloud Platform (GCP) VM instances using Identity-Aware Proxy (IAP) over WebSockets. This project is designed for both standalone CLI use and as a reusable Go library, with robust protocol handling, logging, and test coverage.

---

## Features

- **IAP Tunnel Protocol**: Implements the GCP IAP tunnel protocol over WebSockets.
- **CLI Tool**: Easily forward local ports to remote GCP VM instances.
- **Go Library**: Use as a package in your own Go projects.
- **Pluggable Logging**: Uses Go's `log/slog` for structured logging; users can inject their own logger or disable logging.
- **Unit Tested**: Comprehensive tests using [Testify](https://github.com/stretchr/testify).
- **Linting & Formatting**: Integrated with [golangci-lint](https://golangci-lint.run/) and `gofmt`.
- **Terraform Provider Integration**: Designed to be used as a backend for ephemeral resources in Terraform providers.

---

## Getting Started

### Prerequisites

- Go 1.21 or newer
- [golangci-lint](https://golangci-lint.run/usage/install/) for linting (optional, for development)
- GCP credentials with IAP access to the target VM

### Installation

```sh
go install github.com/davidspek/go-iap-tunnel@latest
```

Clone the repository:

```sh
git clone https://github.com/davidspek/go-iap-tunnel.git
cd go-iap-tunnel
```

Build the CLI:

```sh
go build -o go-iap-tunnel ./cmd/go-iap-tunnel
```

or

```sh
make install
```

---

## Usage

### CLI

```sh
./go-iap-tunnel \
  --project <GCP_PROJECT> \
  --zone <GCP_ZONE> \
  --instance <GCE_INSTANCE> \
  --port <REMOTE_PORT> \
  --listen <LOCAL_ADDR> \
  [--interface nic0] \
  [--network <NETWORK>] \
  [--region <REGION>] \
  [--host <HOST>] \
  [--dest-group <DEST_GROUP>] \
  [--url-override <URL>] \
  [--log-level debug|info|warn|error]
```

**Example:**

```sh
./go-iap-tunnel \
  --project my-gcp-project \
  --zone us-central1-a \
  --instance bastion-vm \
  --port 22 \
  --listen localhost:2222 \
  --log-level info
```

This will forward `localhost:2222` to port 22 on the remote instance via IAP.

---

### As a Go Library

Import and use the package in your Go code:

```go
import (
    tunnel "github.com/davidspek/go-iap-tunnel/pkg"
    "log/slog"
)

func main() {
    // Optionally set a custom logger
    tunnel.SetLogger(slog.Default())

    target := pkg.TunnelTarget{
        Project:  "my-gcp-project",
        Zone:     "us-central1-a",
        Instance: "bastion-vm",
        Port:     22,
    }
    manager := tunnel.NewTunnelManager(target, nil)
    // ... set up listener and call manager.Serve(ctx, listener)
}
```

---

## Logging

This package uses Go's `log/slog` for structured logging.

- **Default:** Logging is disabled (discarded).
- **Custom logger:** Use `SetLogger(*slog.Logger)` to inject your own logger.
- **Disable logging:** `SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))`
- **Log only errors:**

  ```go
  pkg.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
  ```

---

## Development

### Linting

```sh
make lint
```

### Formatting

```sh
make fmt
```

### Unit Tests

```sh
make test
```

### Vulnerability Check

```sh
make vulncheck
```

---

## Project Structure

```text
pkg/                # Library code (protocol, tunnel, token provider, etc.)
  iap_protocol.go   # IAP tunnel protocol implementation
  tunnel.go         # Tunnel manager and adapter
  token_provider.go # OAuth token provider
  log.go            # Logging setup
main.go             # CLI main
Makefile            # Build, lint, test, etc.
.golangci.yaml      # Linter configuration
```

---

## Security

- The package uses OAuth2 for authentication and supports GCP's default credentials.
- All protocol parsing and frame handling is bounds-checked and tested.

---

## Contributing

Pull requests and issues are welcome! Please ensure new code is covered by unit tests and passes linting.

---

## Acknowledgements

- [GCP IAP Documentation](https://cloud.google.com/iap/docs/using-tcp-forwarding)
- [Google Cloud SDK IAP Tunnel](https://github.com/google-cloud-sdk-unofficial/google-cloud-sdk/blob/v517.0.0/lib/googlecloudsdk/api_lib/compute/iap_tunnel_lightweight_websocket.py)
- [golangci-lint](https://golangci-lint.run/)
- [Testify](https://github.com/stretchr/testify)
- [Go slog](https://pkg.go.dev/log/slog)
