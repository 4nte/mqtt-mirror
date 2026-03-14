package internal

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metric collectors for mqtt-mirror.
type Metrics struct {
	MessagesReceived  *prometheus.CounterVec
	MessagesPublished *prometheus.CounterVec
	PublishErrors     prometheus.Counter
	MessageSize       prometheus.Histogram
	PublishDuration   prometheus.Histogram
	SourceConnected   prometheus.Gauge
	TargetConnected   prometheus.Gauge
	BuildInfo         prometheus.Gauge
}

// NewMetrics creates and registers all metrics on the given registry.
func NewMetrics(reg prometheus.Registerer, version string) *Metrics {
	m := &Metrics{
		MessagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mqtt_mirror_messages_received_total",
			Help: "Messages received from source broker.",
		}, []string{"qos"}),

		MessagesPublished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mqtt_mirror_messages_published_total",
			Help: "Messages successfully published to target.",
		}, []string{"qos"}),

		PublishErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "mqtt_mirror_publish_errors_total",
			Help: "Publish failures (timeout or error).",
		}),

		MessageSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "mqtt_mirror_message_size_bytes",
			Help:    "Payload size distribution.",
			Buckets: prometheus.ExponentialBuckets(64, 4, 8), // 64, 256, 1K, 4K, 16K, 64K, 256K, 1M
		}),

		PublishDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "mqtt_mirror_publish_duration_seconds",
			Help:    "Time to publish each message to target.",
			Buckets: prometheus.DefBuckets,
		}),

		SourceConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "mqtt_mirror_source_connected",
			Help: "1 if source connected, 0 otherwise.",
		}),

		TargetConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "mqtt_mirror_target_connected",
			Help: "1 if target connected, 0 otherwise.",
		}),

		BuildInfo: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "mqtt_mirror_build_info",
			Help:        "Build version metadata.",
			ConstLabels: prometheus.Labels{"version": version},
		}),
	}

	m.BuildInfo.Set(1)

	reg.MustRegister(
		m.MessagesReceived,
		m.MessagesPublished,
		m.PublishErrors,
		m.MessageSize,
		m.PublishDuration,
		m.SourceConnected,
		m.TargetConnected,
		m.BuildInfo,
	)

	return m
}
