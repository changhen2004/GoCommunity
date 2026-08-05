package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsEndpointExportsHTTPRequestMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetUpRouter(Dependencies{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", rec.Code, http.StatusOK)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	router.ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want %d", metricsRec.Code, http.StatusOK)
	}

	body := metricsRec.Body.String()
	expected := []string{
		`resource_community_http_requests_total{method="GET",path="/healthz",status="200"}`,
		"resource_community_http_request_duration_seconds_bucket",
	}
	for _, item := range expected {
		if !strings.Contains(body, item) {
			t.Fatalf("metrics body does not contain %q", item)
		}
	}
}
