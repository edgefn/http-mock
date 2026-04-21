package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
  - path: /v1beta/models/{model}:streamGenerateContent
    method: POST
    match:
      query: alt
      equals: sse
    response_file: responses/gemini.sse
    content_type: text/event-stream
`)
	writeFile(t, filepath.Join(dataRoot, "responses", "chat.sse"), "data: hello\n\n")
	writeFile(t, filepath.Join(dataRoot, "responses", "chat.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(dataRoot, "responses", "gemini.sse"), "data: {}\n\n")

	cfg, _, err := Load(dataRoot, "routes.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(cfg.Routes); got != 3 {
		t.Fatalf("routes=%d want=3", got)
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

	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", nil)
	if !cfg.Routes[2].Allows(req, []byte(`{"contents":[]}`)) {
		t.Fatalf("query route should match alt=sse")
	}
	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", nil)
	if cfg.Routes[2].Allows(req, []byte(`{"contents":[]}`)) {
		t.Fatalf("query route should not match without alt=sse")
	}
	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=json", nil)
	if cfg.Routes[2].Allows(req, []byte(`{"contents":[]}`)) {
		t.Fatalf("query route should not match a different alt value")
	}
}

func TestLoadResponseBehaviorFields(t *testing.T) {
	dataRoot := t.TempDir()
	writeFile(t, filepath.Join(dataRoot, "routes.yaml"), `
routes:
  - path: /inline
    method: GET
    body_inline: '{"ok":true}'
    content_type: application/json
    headers:
      x-mock-case: inline
    delay: 10ms
    random_delay:
      min: 2ms
      max: 2ms
    stream_delay: 3ms
`)

	cfg, _, err := Load(dataRoot, "routes.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	route := cfg.Routes[0]
	body, ok := route.InlineBody()
	if !ok || body != `{"ok":true}` {
		t.Fatalf("InlineBody()=(%q,%v), want inline json", body, ok)
	}
	if got := route.Headers["X-Mock-Case"]; got != "inline" {
		t.Fatalf("header=%q want inline", got)
	}
	if got, want := route.DelayDuration(), 10*time.Millisecond; got != want {
		t.Fatalf("DelayDuration()=%s want=%s", got, want)
	}
	if got, want := route.StreamDelayDuration(), 3*time.Millisecond; got != want {
		t.Fatalf("StreamDelayDuration()=%s want=%s", got, want)
	}
	minDelay, maxDelay, ok := route.RandomDelayRange()
	if !ok || minDelay != 2*time.Millisecond || maxDelay != 2*time.Millisecond {
		t.Fatalf("RandomDelayRange()=(%s,%s,%v), want 2ms/2ms", minDelay, maxDelay, ok)
	}
}

func TestLoadRejectsInvalidResponseBehaviorFields(t *testing.T) {
	tests := []struct {
		name    string
		routes  string
		wantErr string
	}{
		{
			name: "missing response source",
			routes: `
routes:
  - path: /missing
    method: GET
`,
			wantErr: "response_file or body_inline is required",
		},
		{
			name: "duplicate response source",
			routes: `
routes:
  - path: /duplicate
    method: GET
    response_file: responses/ok.json
    body_inline: ok
`,
			wantErr: "response_file and body_inline must not both be set",
		},
		{
			name: "invalid delay",
			routes: `
routes:
  - path: /bad-delay
    method: GET
    body_inline: ok
    delay: nope
`,
			wantErr: "delay must be a valid duration",
		},
		{
			name: "negative stream delay",
			routes: `
routes:
  - path: /bad-stream-delay
    method: GET
    body_inline: ok
    stream_delay: -1s
`,
			wantErr: "stream_delay must be >= 0",
		},
		{
			name: "random max before min",
			routes: `
routes:
  - path: /bad-random
    method: GET
    body_inline: ok
    random_delay:
      min: 2s
      max: 1s
`,
			wantErr: "random_delay.max must be greater than or equal to random_delay.min",
		},
		{
			name: "empty header name",
			routes: `
routes:
  - path: /bad-header
    method: GET
    body_inline: ok
    headers:
      "": nope
`,
			wantErr: "headers must not contain empty header names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataRoot := t.TempDir()
			writeFile(t, filepath.Join(dataRoot, "routes.yaml"), tt.routes)

			_, _, err := Load(dataRoot, "routes.yaml")
			if err == nil {
				t.Fatalf("Load succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err=%q want contains %q", err.Error(), tt.wantErr)
			}
		})
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
