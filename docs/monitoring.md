# Metrics & Monitoring

mqtt-mirror exposes Prometheus metrics at `/metrics` on the health port (default `8080`).

## Metrics reference

### `mqtt_mirror_messages_received_total` (Counter)

Messages received from the source broker.

| Label | Values | Description |
|-------|--------|-------------|
| `qos` | `0`, `1`, `2` | MQTT QoS level of the received message |

### `mqtt_mirror_messages_published_total` (Counter)

Messages successfully published to the target broker.

| Label | Values | Description |
|-------|--------|-------------|
| `qos` | `0`, `1`, `2` | MQTT QoS level of the published message |

### `mqtt_mirror_publish_errors_total` (Counter)

Publish failures — either the publish timed out (exceeded `--publish-timeout`) or returned an error. No labels.

### `mqtt_mirror_message_size_bytes` (Histogram)

Payload size distribution of received messages in bytes.

Buckets use exponential scaling (base 64, factor 4, 8 buckets):

| Bucket | Size |
|--------|------|
| 1 | 64 B |
| 2 | 256 B |
| 3 | 1 KB |
| 4 | 4 KB |
| 5 | 16 KB |
| 6 | 64 KB |
| 7 | 256 KB |
| 8 | 1 MB |

### `mqtt_mirror_publish_duration_seconds` (Histogram)

Time to publish each message to the target broker (from `Publish()` call to token completion).

Uses Prometheus default buckets: `.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10`.

### `mqtt_mirror_source_connected` (Gauge)

Source broker connection status. `1` = connected, `0` = disconnected.

### `mqtt_mirror_target_connected` (Gauge)

Target broker connection status. `1` = connected, `0` = disconnected.

### `mqtt_mirror_build_info` (Gauge)

Always `1`. Carries a `version` label with the build version string.

## Standard Go/process metrics

In addition to the above, mqtt-mirror registers the default Go collector (`go_*` metrics) and process collector (`process_*` metrics), providing runtime stats like goroutine count, GC pauses, and memory usage.

## Useful PromQL queries

**Message throughput (received per second):**
```promql
rate(mqtt_mirror_messages_received_total[5m])
```

**Message throughput by QoS:**
```promql
sum by (qos) (rate(mqtt_mirror_messages_received_total[5m]))
```

**Publish error rate:**
```promql
rate(mqtt_mirror_publish_errors_total[5m])
```

**Error ratio (errors as a fraction of received):**
```promql
rate(mqtt_mirror_publish_errors_total[5m])
/ on() rate(mqtt_mirror_messages_received_total[5m])
```

**P99 publish latency:**
```promql
histogram_quantile(0.99, rate(mqtt_mirror_publish_duration_seconds_bucket[5m]))
```

**P50 publish latency:**
```promql
histogram_quantile(0.50, rate(mqtt_mirror_publish_duration_seconds_bucket[5m]))
```

**Average message size:**
```promql
rate(mqtt_mirror_message_size_bytes_sum[5m])
/ rate(mqtt_mirror_message_size_bytes_count[5m])
```

**Broker connection status:**
```promql
mqtt_mirror_source_connected
mqtt_mirror_target_connected
```

## Alerting rules

Example Prometheus alerting rules:

```yaml
groups:
  - name: mqtt-mirror
    rules:
      - alert: MqttMirrorHighErrorRate
        expr: rate(mqtt_mirror_publish_errors_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "mqtt-mirror publish error rate is elevated"
          description: "Error rate {{ $value | humanize }}/s over the last 5 minutes."

      - alert: MqttMirrorSourceDisconnected
        expr: mqtt_mirror_source_connected == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "mqtt-mirror lost connection to source broker"
          description: "Source broker has been disconnected for more than 2 minutes."

      - alert: MqttMirrorTargetDisconnected
        expr: mqtt_mirror_target_connected == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "mqtt-mirror lost connection to target broker"
          description: "Target broker has been disconnected for more than 2 minutes."

      - alert: MqttMirrorHighPublishLatency
        expr: histogram_quantile(0.99, rate(mqtt_mirror_publish_duration_seconds_bucket[5m])) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "mqtt-mirror publish latency is high"
          description: "P99 publish latency is {{ $value | humanize }}s."

      - alert: MqttMirrorNoMessagesReceived
        expr: rate(mqtt_mirror_messages_received_total[10m]) == 0
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "mqtt-mirror is not receiving messages"
          description: "No messages received in the last 10 minutes. Check source broker connectivity and topic filters."
```

## Grafana tips

- Use `mqtt_mirror_build_info` as a join label to show the running version on dashboards.
- Graph `rate(mqtt_mirror_messages_received_total[5m])` and `rate(mqtt_mirror_messages_published_total[5m])` together to spot publish lag — a growing gap between received and published indicates the target broker can't keep up.
- Use a stat panel for `mqtt_mirror_source_connected` and `mqtt_mirror_target_connected` with value mappings (0 = red "Disconnected", 1 = green "Connected").
- Overlay `histogram_quantile(0.50, ...)` and `histogram_quantile(0.99, ...)` for publish latency to see the spread between median and tail latency.
