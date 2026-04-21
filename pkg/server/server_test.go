package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServer_ServesJSONSSEAndBinary(t *testing.T) {
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
    content_type: application/json
  - path: /v1/audio/speech
    method: POST
    response_file: responses/audio.mp3
    content_type: audio/mpeg
  - path: /v1beta/models/{model}:generateContent
    method: POST
    response_file: v1beta/models/{model}:generateContent/text_mock.json
    content_type: application/json
  - path: /v1beta/models/{model}:streamGenerateContent
    method: POST
    match:
      query: alt
      equals: sse
    response_file: v1beta/models/{model}:streamGenerateContent/text_mock.sse
    content_type: text/event-stream
`)
	writeFile(t, filepath.Join(dataRoot, "responses", "chat.sse"), "data: hello\n\ndata: [DONE]\n\n")
	writeFile(t, filepath.Join(dataRoot, "responses", "chat.json"), `{"reply":"ok"}`)
	writeBinary(t, filepath.Join(dataRoot, "responses", "audio.mp3"), []byte{0x01, 0x02, 0x03})
	writeFile(t, filepath.Join(dataRoot, "v1beta", "models", "{model}:generateContent", "text_mock.json"), `{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]}}]}`)
	writeFile(t, filepath.Join(dataRoot, "v1beta", "models", "{model}:streamGenerateContent", "text_mock.sse"), `data: {"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}

`)

	srv, err := Load(dataRoot, "routes.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":false}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"reply":"ok"}` {
		t.Fatalf("unexpected chat response code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("unexpected sse response code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"input":"hi"}`))
	srv.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("code=%d want=%d", got, want)
	}
	if got := rec.Body.Bytes(); len(got) != 3 || got[0] != 0x01 {
		t.Fatalf("unexpected audio bytes: %v", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", strings.NewReader(`{"contents":[]}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello from gemini") {
		t.Fatalf("unexpected gemini response code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[]}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "data:") {
		t.Fatalf("unexpected gemini stream response code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", strings.NewReader(`{"contents":[]}`))
	srv.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("gemini stream without alt code=%d want=%d", got, want)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":"maybe"}`))
	srv.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("fallback code=%d want=%d", got, want)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeBinary(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
