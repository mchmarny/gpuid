package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mchmarny/gpuid/pkg/logger"
)

func getTestServer(t *testing.T) *server {
	t.Helper()
	logger := logger.NewTestLogger(t)
	return &server{logger: logger, port: 8080}
}

func TestBuildHandler_HealthAndReadyEndpoints(t *testing.T) {
	srv := getTestServer(t)
	handler := srv.buildHandler(nil)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	endpoints := []string{"/healthz", "/readyz", "/"}
	for _, ep := range endpoints {
		resp, err := http.Get(ts.URL + ep)
		if err != nil {
			t.Fatalf("failed to GET %s: %v", ep, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK for %s, got %d", ep, resp.StatusCode)
		}
	}
}

func TestBuildHandler_RegistersMetricsHandler(t *testing.T) {
	srv := getTestServer(t)
	metricsCalled := false
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		metricsCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := srv.buildHandler(map[string]http.Handler{"/metrics": metricsHandler})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /metrics, got %d", resp.StatusCode)
	}
	if !metricsCalled {
		t.Error("metrics handler was not called")
	}
}

func TestWithLogger_SetsLogger(t *testing.T) {
	s := &server{}
	l := logger.NewTestLogger(t)
	WithLogger(l)(s)
	if s.logger != l {
		t.Error("WithLogger did not set logger")
	}
}

func TestWithPort_SetsPort(t *testing.T) {
	s := &server{}
	WithPort(1234)(s)
	if s.port != 1234 {
		t.Error("WithPort did not set port")
	}
}

func TestNewServer_DefaultsAndOptions(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Error("NewServer returned nil")
	}
	custom := NewServer(WithLogger(logger.NewTestLogger(t)), WithPort(4321))
	// Type assertion to access fields
	impl, ok := custom.(*server)
	if !ok {
		t.Error("NewServer did not return *server type")
	}
	if impl.port != 4321 {
		t.Errorf("expected port 4321, got %d", impl.port)
	}
}

// TestServer_Serve tests the server's ability to start and handle requests
func TestServer_Serve(t *testing.T) {
	log := logger.NewTestLogger(t)

	// Find an available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	server := NewServer(WithLogger(log), WithPort(port))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start server in goroutine
	done := make(chan bool)
	go func() {
		defer close(done)
		server.Serve(ctx, map[string]http.Handler{
			"/test": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "test response")
			}),
		})
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test health endpoints
	client := &http.Client{Timeout: time.Second}

	endpoints := []string{"/healthz", "/readyz", "/"}
	for _, endpoint := range endpoints {
		url := fmt.Sprintf("http://localhost:%d%s", port, endpoint)
		response, getErr := client.Get(url)
		if getErr != nil {
			t.Errorf("Failed to connect to %s: %v", endpoint, getErr)
			continue
		}
		response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for %s, got %d", endpoint, response.StatusCode)
		}
	}

	// Test custom handler
	testURL := fmt.Sprintf("http://localhost:%d/test", port)
	testResp, err := client.Get(testURL)
	if err != nil {
		t.Errorf("Failed to connect to /test: %v", err)
	} else {
		testResp.Body.Close()
		if testResp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /test, got %d", testResp.StatusCode)
		}
	}

	// Cancel context to stop server
	cancel()

	// Wait for server to shutdown
	select {
	case <-done:
		// Server stopped successfully
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop within timeout")
	}

	// Give extra time for any goroutine cleanup to complete
	time.Sleep(100 * time.Millisecond)
}

// TestServer_ServeWithNilHandlers tests server with nil handlers map
func TestServer_ServeWithNilHandlers(t *testing.T) {
	log := logger.NewTestLogger(t)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	server := NewServer(WithLogger(log), WithPort(port))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		defer close(done)
		server.Serve(ctx, nil) // Pass nil handlers
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Test that health endpoints still work
	client := &http.Client{Timeout: 100 * time.Millisecond}
	url := fmt.Sprintf("http://localhost:%d/healthz", port)
	resp, err := client.Get(url)
	if err != nil {
		t.Errorf("Failed to connect to /healthz: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /healthz, got %d", resp.StatusCode)
		}
	}

	cancel()

	// Wait for server to shutdown with timeout
	select {
	case <-done:
		// Server stopped successfully
	case <-time.After(500 * time.Millisecond):
		t.Error("Server did not stop within timeout")
	}

	// Give extra time for any goroutine cleanup to complete
	time.Sleep(100 * time.Millisecond)
}
