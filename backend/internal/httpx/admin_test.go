package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAdmin(t *testing.T) {
	const token = "0123456789abcdef01234567"
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RequireAdmin(token, "/api/admin/")(ok)

	tests := []struct {
		name, path, auth string
		want             int
	}{
		{"valid token", "/api/admin/llm-settings", "Bearer " + token, http.StatusNoContent},
		{"missing header", "/api/admin/llm-settings", "", http.StatusUnauthorized},
		{"wrong token", "/api/admin/llm-settings", "Bearer " + token + "x", http.StatusUnauthorized},
		{"prefix of token", "/api/admin/llm-settings", "Bearer " + token[:10], http.StatusUnauthorized},
		{"wrong scheme", "/api/admin/llm-settings", "Basic " + token, http.StatusUnauthorized},
		{"new admin route is protected", "/api/admin/anything", "", http.StatusUnauthorized},
		{"public route", "/api/leads", "", http.StatusNoContent},
		{"lookalike path", "/api/administrators", "", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("got %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
