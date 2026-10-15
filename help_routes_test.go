package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHelpRoutesRequireAuthentication(t *testing.T) {
	server := &Server{}
	handler := server.routes()

	tests := []struct {
		path string
		want int
	}{
		{"/help", http.StatusSeeOther},
		{"/api/help", http.StatusUnauthorized},
		{"/api/help/CASHIER_GUIDE", http.StatusUnauthorized},
		{"/api/help/DEPLOYMENT", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf(
					"%s returned %d; want %d; body=%s",
					tt.path,
					rec.Code,
					tt.want,
					rec.Body.String(),
				)
			}
		})
	}
}
