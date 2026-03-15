# Troubleshooting

## `/readyz` returning 503

The readiness endpoint returns 503 when either the source or target MQTT client is not connected.

**Diagnosis:**
1. Check logs for `connection lost` or `connection timeout exceeded` messages.
2. Check metrics: `mqtt_mirror_source_connected` and `mqtt_mirror_target_connected` — the one showing `0` identifies which broker is unreachable.
3. Verify broker URIs are correct and the brokers are reachable from the pod (network policies, DNS resolution, firewall rules).
4. If using authentication, confirm credentials are correct — look for `connection failed` in the logs.

**Note:** On startup, `/readyz` will return 503 until both connections are established. The readiness probe has `initialDelaySeconds: 5` by default, giving time for initial connection.

## Messages not appearing on target

**Check the basics:**
1. Confirm mqtt-mirror is receiving messages: `mqtt_mirror_messages_received_total` should be increasing. If not, the issue is on the source side (wrong topic filter, no messages being published to source, authentication issues).
2. Check for publish errors: `mqtt_mirror_publish_errors_total`. If this is increasing, messages are being received but failing to publish.
3. Check publish latency: if `mqtt_mirror_publish_duration_seconds` shows values close to the `--publish-timeout` (default 10s), the target broker may be overloaded or unreachable.

**Topic filter issues:**
- Filters are comma-separated. `--topic-filter "devices/#"` subscribes to all topics under `devices/`. An empty filter (the default) subscribes to `#` (all topics).
- Verify the filter matches what you expect with a test subscriber: `mosquitto_sub -h source-broker -t "your/filter/#"`.

**Topic rewriting confusion:**
- If using `--topic-prefix` or `--topic-replace`, messages are published to the *transformed* topic. Enable verbose logging to see both the original and rewritten topic in the logs.
- Replacements are applied first, then the prefix.

## Persistent session message loss after restart

If using `--clean-session=false` but messages are lost during restarts:

1. **Client ID must be stable.** If `--name` is not set, a random 8-character ID is generated on each restart, creating a new session every time. Always set `--name` with persistent sessions.
2. **Broker must support persistent sessions.** Not all brokers retain sessions — check your broker's configuration for session expiry/TTL settings.
3. **QoS 0 messages are never queued.** The broker only queues messages for persistent sessions at QoS 1 or 2. QoS 0 messages sent while the client is disconnected are lost regardless of session persistence.

## High publish error rate

If `mqtt_mirror_publish_errors_total` is increasing:

1. **Timeout errors** (logged as `publish timed out`): The target broker is not acknowledging publishes within the `--publish-timeout` window (default 10s). This usually means the target broker is overloaded, the network is slow, or the connection is degraded. Consider increasing `--publish-timeout` if latency is expected to be high.
2. **Publish errors** (logged as `publish failed` with error details): Check the specific error in the logs. Common causes include authorization failures (no write permission on target topics) or the target broker rejecting the message.

## Connection lifecycle

Understanding how mqtt-mirror handles connections helps diagnose connectivity issues:

1. **Initial connection**: Both source and target connections have a 15-second timeout. If the broker is unreachable at startup, mqtt-mirror exits with an error (it does not retry initial connections).
2. **Auto-reconnect**: After the initial connection, if a connection drops, the Paho client automatically reconnects with exponential backoff up to a 15-second maximum interval.
3. **Auto-resubscribe**: On source broker reconnection, subscriptions are automatically re-established via the `OnConnect` handler. No manual intervention is needed.
4. **Graceful shutdown**: On SIGINT or SIGTERM, mqtt-mirror disconnects from both brokers with a 250ms drain period. The health server shuts down with a 5-second timeout. A second signal forces an immediate exit.

## Verbose logging

Enable verbose logging (`--verbose` / `-v`) for detailed per-message output:

```
INFO  message replicated  {"bytes_len": 42, "topic": "devices/sensor1/data", "QoS": 1, "retained": false}
```

When topic rewriting is active, a `rewritten_topic` field is added:

```
INFO  message replicated  {"bytes_len": 42, "topic": "devices/sensor1/data", "QoS": 1, "retained": false, "rewritten_topic": "mirror/devices/sensor1/data"}
```

Verbose mode uses Zap's development logger (human-readable, colored output). Non-verbose mode uses the production logger (JSON format), which is better suited for log aggregation.

## Metrics-based diagnostics

Quick checklist using metrics:

| Symptom | Metric to check | What to look for |
|---------|-----------------|------------------|
| No messages flowing | `mqtt_mirror_messages_received_total` | Rate is 0 — source subscription or connectivity issue |
| Messages received but not published | `mqtt_mirror_publish_errors_total` | Increasing — check logs for timeout vs. error |
| High latency | `mqtt_mirror_publish_duration_seconds` | P99 approaching `--publish-timeout` value |
| Broker disconnected | `mqtt_mirror_source_connected` / `mqtt_mirror_target_connected` | Value is 0 |
| Version mismatch | `mqtt_mirror_build_info` | Check `version` label |
