package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vmkteam/appkit"
)

func TestRegisterDownloadHandlers(t *testing.T) {
	dir := t.TempDir()
	const name = "reviewctl-linux-amd64"
	const body = "fake-binary-bytes"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a := &App{echo: appkit.NewEcho()}
	a.cfg.Server.DownloadDir = dir
	a.registerDownloadHandlers()

	tests := []struct {
		name     string
		path     string
		wantCode int
		wantBody string
	}{
		{"existing file", "/download/" + name, http.StatusOK, body},
		{"missing file", "/download/reviewctl-does-not-exist", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			a.echo.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRegisterDownloadHandlers_DisabledWhenEmpty(t *testing.T) {
	a := &App{echo: appkit.NewEcho()}
	a.cfg.Server.DownloadDir = ""
	a.registerDownloadHandlers()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/download/anything", nil)
	a.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route must not be registered)", rec.Code)
	}
}
