package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardRoutes(t *testing.T) {
	for _, route := range []string{"/", "/requests", "/playground"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest("GET", route, nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), "<html") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid page at %s: %d", route, response.Code)
		}
	}
	for _, route := range []string{"/api/missing", "/assets/missing.js", "/not-a-route"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest("GET", route, nil))
		if response.Code != 404 {
			t.Fatalf("%s: got %d", route, response.Code)
		}
	}
}
