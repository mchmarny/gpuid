package counter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCounter_Inc_Table(t *testing.T) {
	tests := []struct {
		name       string
		help       string
		labelName  string
		labelValue string
	}{
		{"success", "success", "status", "success"},
		{"failure", "failure", "status", "failure"},
		{"pending", "pending", "status", "pending"},
	}
	for _, tt := range tests {
		c := New(tt.name, tt.help, tt.labelName)
		t.Run(tt.name, func(t *testing.T) {
			if c == nil {
				t.Fatalf("NewCounter(%q, %q, %q) returned nil", tt.name, tt.help, tt.labelName)
			}
			c.Increment(tt.labelValue)
		})
	}
}

// TestHandler tests the Handler method for serving Prometheus metrics
func TestHandler(t *testing.T) {
	// Create a test counter to ensure metrics exist
	counter := New("test_handler_metric", "Test metric for handler", "status")
	counter.Increment("success")

	handler := Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	// Test the handler with an HTTP request
	req, err := http.NewRequest("GET", "/metrics", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Check that we get a 200 status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check that the response contains Prometheus metrics format
	body := rr.Body.String()
	if !strings.Contains(body, "# HELP") {
		t.Error("Response does not contain Prometheus help comments")
	}

	if !strings.Contains(body, "# TYPE") {
		t.Error("Response does not contain Prometheus type comments")
	}

	// Check that our test metric appears in the output
	if !strings.Contains(body, "test_handler_metric") {
		t.Error("Response does not contain our test metric")
	}

	t.Log("Handler correctly serves Prometheus metrics")
}
