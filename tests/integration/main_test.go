//go:build integration
// +build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
)

func TestHealthz(t *testing.T) {
	baseURL := envOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	resp, err := http.Get(fmt.Sprintf("%s/healthz", baseURL))
	if err != nil {
		t.Fatalf("health check request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
}

func TestMatchFlowSimulation(t *testing.T) {
	t.Skip("Match flow simulation handled via WebSocket integration test")
}
