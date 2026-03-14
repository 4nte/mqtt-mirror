## Highlights

- **Health endpoints** — `/healthz` and `/readyz` HTTP probes for Kubernetes deployments
- **Prometheus metrics** — message throughput counters (by QoS), publish error counts, payload size histograms, publish duration histograms, connection state gauges, and build info on `/metrics`
- **npx support** — `npx mqtt-mirror@latest` for zero-install usage via platform-specific npm packages
- **GHCR images** — Docker images now published to `ghcr.io/4nte/mqtt-mirror` alongside DockerHub

## Features

- Add HTTP health server with liveness (`/healthz`) and readiness (`/readyz`) probes
- Add Prometheus metrics endpoint (`/metrics`) with message, error, and connection gauges
- Add `npx mqtt-mirror@latest` support with platform-specific npm packages
- Publish Docker images to GitHub Container Registry (GHCR)
- Add `--version` flag with build-time version injection

## Helm Chart

- Move broker credentials to Kubernetes Secrets (with `existingSecret` support)
- Add security contexts: `runAsNonRoot`, drop all capabilities, read-only root filesystem
- Add default resource requests/limits
- Add optional `ServiceMonitor` for Prometheus scraping
- Add `imagePullSecrets`, `podAnnotations`, and `extraEnvVars` support

## Bug Fixes

- Fix MQTT password parsing with special characters (`#`, `@`, `:`) (#8)
- Fix `.gitignore` to not ignore npm wrapper scripts

## Maintenance

- Modernize codebase to Go 1.26 (`strings.Cut`, `RunE`, `slices.ContainsFunc`)
- Upgrade `docker/docker` to v28.5.2 (security fix for Dependabot alert #113)
- Add comprehensive edge-case and resilience tests
- Add Kubernetes E2E tests using K3s testcontainers
- Fix all golangci-lint errors
- Remove build artifacts from version control
