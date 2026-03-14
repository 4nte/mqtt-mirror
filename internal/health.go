package internal

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// HealthServer provides HTTP health check endpoints for Kubernetes probes.
type HealthServer struct {
	mu     sync.RWMutex
	source paho.Client
	target paho.Client
	server *http.Server
}

// NewHealthServer creates a new HealthServer.
func NewHealthServer() *HealthServer {
	return &HealthServer{}
}

// SetClients registers the source and target MQTT clients for readiness checks.
func (h *HealthServer) SetClients(source, target paho.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.source = source
	h.target = target
}

// IsReady returns true when both MQTT clients are connected.
func (h *HealthServer) IsReady() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.source == nil || h.target == nil {
		return false
	}
	return h.source.IsConnected() && h.target.IsConnected()
}

// Start begins serving health check endpoints on the given port.
// If reg is non-nil, a /metrics endpoint is registered for Prometheus scraping.
func (h *HealthServer) Start(port int, reg *prometheus.Registry) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if h.IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "not ready")
		}
	})

	if reg != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	}

	h.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		zap.L().Info("health server starting", zap.Int("port", port))
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Error("health server error", zap.Error(err))
		}
	}()
}

// Shutdown gracefully stops the health server.
func (h *HealthServer) Shutdown(ctx context.Context) error {
	if h.server == nil {
		return nil
	}
	return h.server.Shutdown(ctx)
}
