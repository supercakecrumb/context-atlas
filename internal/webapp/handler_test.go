package webapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerServesAssetAndSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "app-shell", "assets/app.js": "javascript"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(os.DirFS(dir))

	for _, tc := range []struct {
		path      string
		wantBody  string
		wantCache string
	}{
		{path: "/assets/app.js", wantBody: "javascript", wantCache: "public, max-age=31536000, immutable"},
		{path: "/explore", wantBody: "app-shell", wantCache: "no-cache"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Body.String() != tc.wantBody {
			t.Fatalf("%s: status=%d body=%q", tc.path, res.Code, res.Body.String())
		}
		if got := res.Header().Get("Cache-Control"); got != tc.wantCache {
			t.Fatalf("%s cache = %q", tc.path, got)
		}
		if res.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("%s missing CSP", tc.path)
		}
	}
}
