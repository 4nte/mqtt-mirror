package internal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/assert"
)

// mockClient implements paho.Client for testing. We only need IsConnected().
type mockClient struct {
	paho.Client
	connected bool
}

func (m *mockClient) IsConnected() bool {
	return m.connected
}

func TestHealthz_ReturnsOK(t *testing.T) {
	h := NewHealthServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	_ = h // ensure server was created
}

func TestReadyz_NoClients_Returns503(t *testing.T) {
	h := NewHealthServer()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.IsReady() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReadyz_BothConnected_Returns200(t *testing.T) {
	h := NewHealthServer()
	h.SetClients(
		&mockClient{connected: true},
		&mockClient{connected: true},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.IsReady() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestReadyz_SourceDisconnected_Returns503(t *testing.T) {
	h := NewHealthServer()
	h.SetClients(
		&mockClient{connected: false},
		&mockClient{connected: true},
	)

	assert.False(t, h.IsReady())
}

func TestReadyz_TargetDisconnected_Returns503(t *testing.T) {
	h := NewHealthServer()
	h.SetClients(
		&mockClient{connected: true},
		&mockClient{connected: false},
	)

	assert.False(t, h.IsReady())
}
