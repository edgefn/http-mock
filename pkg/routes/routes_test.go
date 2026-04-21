package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndMatchRoutes(t *testing.T) {
	dataRoot := t.TempDir()
	writeFile(t, filepath.Join(dataRoot, "routes.yaml"), `
routes:
  - path: /v1/chat/completions
    method: POST
    match:
      json_path: stream
      equals: "true"
    response_file: responses/chat.sse
    content_type: text/event-stream
  - path: /v1/chat/completions
    method: POST
    response_file: responses/chat.json
`)
	writeFile(t, filepath.Join(dataRoot, "responses", "chat.sse"), "data: hello\n\n")
	writeFile(t, filepath.Join(dataRoot, "responses", "chat.json"), `{"ok":true}`)

	cfg, _, err := Load(dataRoot, "routes.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(cfg.Routes); got != 2 {
		t.Fatalf("routes=%d want=2", got)
	}
	if got, want := cfg.Routes[1].ContentType, "application/json"; got != want {
		t.Fatalf("content_type=%q want=%q", got, want)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if !cfg.Routes[1].Allows(req, []byte(`{"stream":false}`)) {
		t.Fatalf("fallback route should match")
	}
	if cfg.Routes[0].Allows(req, []byte(`{"stream":false}`)) {
		t.Fatalf("stream route should not match false")
	}
	if !cfg.Routes[0].Allows(req, []byte(`{"stream":true}`)) {
		t.Fatalf("stream route should match true")
	}
}

func TestRouteMatchesTemplatePath(t *testing.T) {
	route := Route{Path: "/v1beta/models/{model}:generateContent"}

	if !route.MatchesPath("/v1beta/models/gemini-2.5-flash:generateContent") {
		t.Fatalf("template route should match gemini generateContent path")
	}
	if route.MatchesPath("/v1beta/models/:generateContent") {
		t.Fatalf("template route should not match empty model")
	}
	if route.MatchesPath("/v1beta/models/gemini-2.5-flash:streamGenerateContent") {
		t.Fatalf("template route should not match a different action")
	}
	if route.MatchesPath("/v1beta/models/gemini/2.5-flash:generateContent") {
		t.Fatalf("template route should not match across path segments")
	}
	if (Route{Path: "/v1/chat/completions"}).MatchesPath("/v1/chat/completion") {
		t.Fatalf("exact route should keep exact path matching")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
