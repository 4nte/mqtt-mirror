# Kubernetes & Helm Deployment

## Quick start

```bash
helm repo add 4nte https://4nte.github.io/helm-charts/
helm install mqtt-mirror 4nte/mqtt-mirror \
  --set mqtt.source=tcp://source-broker:1883 \
  --set mqtt.target=tcp://target-broker:1883
```

## Helm values reference

### Image

| Parameter | Default | Description |
|-----------|---------|-------------|
| `image.repository` | `antegulin/mqtt-mirror` | Container image repository |
| `image.tag` | `""` (Chart appVersion) | Image tag override |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | `[]` | Image pull secrets for private registries |

### MQTT

| Parameter | Default | Description |
|-----------|---------|-------------|
| `mqtt.source` | `""` | Source broker URI (required) |
| `mqtt.target` | `""` | Target broker URI (required) |
| `mqtt.topic_filter` | `[]` (all topics) | List of topic filters |
| `mqtt.topic_prefix` | `""` | Prefix to prepend to mirrored topics |
| `mqtt.topic_replace` | `[]` | Topic replacements in `old:new` format |
| `mqtt.publish_timeout` | `""` | Timeout for publishing messages (e.g. `"10s"`) |
| `mqtt.clean_session` | `true` | MQTT clean session flag |
| `mqtt.existingSecret` | (commented out) | Name of an existing Secret with `SOURCE` and `TARGET` keys |

### Instance

| Parameter | Default | Description |
|-----------|---------|-------------|
| `name` | `""` | Instance name for MQTT client ID (max 23 chars, random if empty) |
| `verbose` | `false` | Enable verbose logging |
| `replicaCount` | `1` | Number of replicas (see [Replica count](#why-replicacount-should-stay-at-1)) |

### Health & monitoring

| Parameter | Default | Description |
|-----------|---------|-------------|
| `health.port` | `8080` | Port for health check and metrics HTTP server |
| `serviceMonitor.enabled` | `false` | Create a Prometheus ServiceMonitor resource |
| `serviceMonitor.interval` | `30s` | Scrape interval |
| `serviceMonitor.labels` | `{}` | Extra labels for ServiceMonitor (e.g. for Prometheus selector matching) |

### ConfigMap-based configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `configFile` | (commented out) | Inline TOML config mounted at `/etc/mqtt-mirror/mirror.toml` |

### Kubernetes

| Parameter | Default | Description |
|-----------|---------|-------------|
| `nameOverride` | `""` | Override chart name |
| `fullnameOverride` | `""` | Override fully qualified app name |
| `podAnnotations` | `{}` | Extra annotations on the Pod |
| `extraEnvVars` | `[]` | Additional environment variables for the container |
| `nodeSelector` | `{}` | Node selector constraints |
| `tolerations` | `[]` | Pod tolerations |
| `affinity` | `{}` | Pod affinity rules |

### Resources & security

| Parameter | Default | Description |
|-----------|---------|-------------|
| `resources.requests.cpu` | `50m` | CPU request |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.memory` | `128Mi` | Memory limit |
| `securityContext.runAsNonRoot` | `true` | Require non-root user |
| `securityContext.runAsUser` | `65534` | User ID (nobody) |
| `securityContext.fsGroup` | `65534` | Filesystem group |
| `containerSecurityContext.allowPrivilegeEscalation` | `false` | Block privilege escalation |
| `containerSecurityContext.readOnlyRootFilesystem` | `true` | Read-only root filesystem |
| `containerSecurityContext.capabilities.drop` | `[ALL]` | Drop all Linux capabilities |

## ConfigMap-based configuration

For complex configurations, use `configFile` to provide inline TOML instead of passing everything through environment variables. Source and target broker URIs are still read from the Secret — all other options go in the ConfigMap.

```yaml
configFile: |
  verbose = true
  topic_filter = ["devices/#", "sensors/+/temperature"]
  name = "my-mirror"
  health_port = 9090
  clean_session = false
  topic_prefix = "mirror/"
  topic_replace = ["staging:production", "v1:v2"]
  publish_timeout = "10s"
```

When `configFile` is set, the chart:
1. Creates a ConfigMap with the TOML content
2. Mounts it at `/etc/mqtt-mirror/mirror.toml`
3. Passes `--config /etc/mqtt-mirror/mirror.toml` to the container
4. Adds a `checksum/config` pod annotation so that config changes trigger a rollout

## Using existing secrets

If you manage secrets externally (e.g., Sealed Secrets, External Secrets Operator), set `mqtt.existingSecret` to the name of a Secret containing `SOURCE` and `TARGET` keys:

```yaml
mqtt:
  existingSecret: my-broker-credentials
```

The Secret must have this shape:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-broker-credentials
type: Opaque
data:
  SOURCE: <base64-encoded source URI>
  TARGET: <base64-encoded target URI>
```

When `existingSecret` is set, the chart skips creating its own Secret.

## ServiceMonitor setup

To enable Prometheus Operator scraping:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  labels:
    release: prometheus  # match your Prometheus selector
```

This creates a ServiceMonitor that scrapes `/metrics` on the health port. Make sure the `labels` match your Prometheus instance's `serviceMonitorSelector`.

## Why `replicaCount` should stay at 1

Running multiple replicas causes **duplicate message delivery** — each replica subscribes to the same topics on the source broker, so every message gets published N times to the target. Keep `replicaCount: 1`.

If you need high availability, use persistent sessions (`clean_session: false` with a stable `name`) so the broker queues messages during brief downtime, combined with Kubernetes' automatic pod restart.

## Deployment strategy

When `clean_session` is `false`, the chart automatically sets `strategy.type: Recreate` instead of the default `RollingUpdate`. This prevents two pods from connecting with the same client ID simultaneously, which would cause the broker to disconnect one of them in a loop.

## Probes

The deployment configures both liveness and readiness probes:

- **Liveness** (`/healthz`): Always returns 200. If this fails, the pod is restarted.
- **Readiness** (`/readyz`): Returns 200 only when both source and target brokers are connected. Traffic (e.g., from ServiceMonitor scrapes) is only sent to ready pods.

Both probes use:
- `initialDelaySeconds: 5`
- `periodSeconds: 10`
- `failureThreshold: 3`

## Security context

The default security context runs the container as the `nobody` user (UID 65534) with a read-only root filesystem and all Linux capabilities dropped. This follows the principle of least privilege — mqtt-mirror only needs network access, no filesystem writes or elevated permissions.

## Resource sizing

The defaults (`50m` CPU, `64Mi` request / `128Mi` limit memory) work well for low-to-moderate message rates. For high-throughput scenarios (thousands of messages/second), monitor actual usage via `/metrics` and adjust accordingly. mqtt-mirror is lightweight — memory usage primarily scales with message payload sizes and in-flight publish operations.
