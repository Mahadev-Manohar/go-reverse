# GoReverse — Concurrent Reverse Proxy & Load Balancer

GoReverse is a concurrent HTTP reverse proxy and load balancer built in Go. It distributes incoming requests across multiple backend servers using round-robin routing, monitors backend health, and automatically fails over to healthy servers when failures occur.

The project focuses on backend engineering concepts such as concurrency safety, fault tolerance, request timeouts, rate limiting, observability, and containerized deployment.

## Features

- **Reverse Proxy:** Forwards HTTP requests to backend servers and returns their responses, including headers and status codes.
- **Round-Robin Load Balancing:** Distributes requests across healthy backend servers.
- **Health Monitoring:** Periodically checks backend availability and updates backend health status.
- **Automatic Failover:** Marks failed backends unhealthy and retries eligible requests against other healthy backends.
- **Safe-Method Retries:** Retries eligible `GET`, `HEAD`, and `OPTIONS` requests without retrying unsafe HTTP methods.
- **Concurrency Safety:** Uses Go mutexes to protect shared backend-pool, metrics, and rate-limiter state.
- **Request Timeouts:** Applies timeouts to backend requests and health checks.
- **IP-Based Rate Limiting:** Implements a token-bucket rate limiter with configurable request rates and burst capacity.
- **Structured Logging:** Produces structured JSON logs containing request IDs, backend information, status codes, and request duration.
- **Prometheus-Compatible Metrics:** Exposes request counts, failures, retries, backend health, and latency metrics through `/metrics`.
- **Configuration:** Supports YAML configuration and environment-variable overrides.
- **Graceful Shutdown:** Stops the HTTP server and health-checking goroutine during shutdown.
- **Containerization:** Packages the proxy and backend servers using Docker and Docker Compose.
- **Automated Tests:** Tests round-robin routing, rate limiting, and backend failure handling.

## Architecture

```text
                   Client
                     |
                     v
              +---------------+
              |   GoReverse   |
              |  HTTP Proxy   |
              +---------------+
                     |
             Rate Limiting
                     |
             Round-Robin Pool
                     |
          +----------+----------+
          |          |          |
          v          v          v
      Backend 1  Backend 2  Backend 3
          |          |          |
          +----------+----------+
                     ^
                     |
             Health Monitoring

          /metrics --> Metrics Endpoint
```

The proxy selects a healthy backend for each request. If a backend request fails, GoReverse marks that backend unhealthy and attempts another eligible backend when the HTTP method permits retries. The health checker periodically checks backend availability so recovered backends can rejoin the rotation.

## Tech Stack

- **Language:** Go
- **Networking:** Go `net/http`
- **Concurrency:** Goroutines, mutexes, context cancellation
- **Configuration:** YAML and environment variables
- **Observability:** Structured logging and Prometheus-compatible metrics
- **Deployment:** Docker and Docker Compose
- **Testing:** Go `testing`, `httptest`, race detector

## Project Structure

```text
go-reverse/
├── cmd/
│   ├── backend/
│   │   └── main.go
│   └── proxy/
│       ├── main.go
│       ├── config.go
│       ├── ratelimiter.go
│       └── main_test.go
├── config.yaml
├── Dockerfile.backend
├── Dockerfile.proxy
├── compose.yaml
├── go.mod
├── go.sum
└── README.md
```

## Getting Started

### Prerequisites

- Go version compatible with the project's `go.mod`
- Docker Desktop or Docker Engine
- Docker Compose

### Run with Docker Compose

1. Clone the repository:

   ```bash
   git clone <YOUR_GITHUB_REPOSITORY_URL>
   cd go-reverse
   ```

2. Build the images and start the proxy and backend servers:

   ```bash
   docker compose up --build -d
   ```

3. Check that the containers are running:

   ```bash
   docker compose ps
   ```

4. Send requests through the proxy:

   ```bash
   curl http://localhost:8080/hello
   ```

   Repeat the request to observe round-robin routing across the healthy backend servers.

5. Inspect the metrics:

   ```bash
   curl http://localhost:8080/metrics
   ```

6. View the logs:

   ```bash
   docker compose logs -f proxy
   ```

### Stop the application

```bash
docker compose down
```

## Configuration

GoReverse loads its configuration from `config.yaml` by default. The configuration defines the server settings, backend servers, and rate-limiting options.

The following environment variables can override supported settings:

| Environment variable | Purpose |
|---|---|
| `GOREVERSE_CONFIG` | Path to the YAML configuration file |
| `GOREVERSE_PORT` | Proxy listening port |
| `GOREVERSE_RATE_LIMIT_ENABLED` | Enable or disable rate limiting |
| `GOREVERSE_RATE_LIMIT_RPS` | Rate-limiting requests per second |
| `GOREVERSE_RATE_LIMIT_BURST` | Maximum token-bucket burst capacity |

When running with Docker Compose, configure backend URLs using the Compose service names. When running the proxy directly on your host, configure the backend URLs to point to the host-accessible backend addresses.

## Observability

GoReverse exposes a `/metrics` endpoint in Prometheus text exposition format.

| Metric | Description |
|---|---|
| `goreverse_requests_total` | Total requests received |
| `goreverse_requests_success_total` | Successfully forwarded requests |
| `goreverse_requests_failed_total` | Failed requests |
| `goreverse_retries_total` | Retry attempts |
| `goreverse_backend_requests_total` | Requests handled by each backend |
| `goreverse_backend_failures_total` | Backend request failures |
| `goreverse_backend_healthy` | Backend health status |
| `goreverse_request_duration_seconds` | Request latency histogram |
| `goreverse_health_checks_total` | Health checks performed |
| `goreverse_health_check_failures_total` | Failed health checks |

These metrics can be scraped by a Prometheus server. The application exposes the metrics endpoint; a separate Prometheus or Grafana deployment is not required to run GoReverse.

## Testing

Run all automated tests:

```bash
go test ./...
```

Run the proxy tests with verbose output:

```bash
go test ./cmd/proxy -v
```

Run the race detector:

```bash
go test -race ./...
```

### Automated test coverage

- **Round-Robin Routing:** Verifies that healthy backends are selected in rotation.
- **Rate Limiting:** Verifies that requests exceeding the configured burst capacity receive HTTP `429 Too Many Requests` and a `Retry-After` header.
- **Backend Failure Handling:** Simulates an unavailable backend and verifies that the proxy retries a healthy backend, marks the failed backend unhealthy, and records the failure and retry metrics.

## Key Engineering Concepts

GoReverse demonstrates practical experience with:

- HTTP request and response forwarding
- Load balancing and backend selection
- Fault tolerance and failure recovery
- Concurrent programming and synchronization
- Token-bucket rate limiting
- Timeouts and safe-method retry policies
- Health checks and observability
- Configuration management
- Containerized deployment
- Automated testing and race detection

## Future Improvements

Potential extensions include integration testing under concurrent load, benchmark-driven performance analysis, and a dedicated Prometheus/Grafana monitoring deployment.

## License

Choose a license before distributing the project publicly. If you intend to use an open-source license, add the corresponding `LICENSE` file to the repository.