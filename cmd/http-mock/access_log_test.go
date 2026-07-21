package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAccessLog_RecordsRequestAndImplicitStatus(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	handler := accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), logger)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses?stream=true", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status=%d want=%d", got, want)
	}
	pattern := `^\[HTTP-MOCK\] \|\s+200 \|\s+\S+ \|\s+192\.0\.2\.10 \| POST\s+"/v1/responses\?stream=true"\n$`
	if !regexp.MustCompile(pattern).MatchString(output.String()) {
		t.Fatalf("unexpected access log: %q", output.String())
	}
}

func TestAccessLog_RecordsFirstExplicitStatus(t *testing.T) {
	var output bytes.Buffer
	handler := accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError)
	}), log.New(&output, "", 0))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/resource", nil))

	if got, want := recorder.Code, http.StatusCreated; got != want {
		t.Fatalf("status=%d want=%d", got, want)
	}
	if !strings.Contains(output.String(), "| 201 |") {
		t.Fatalf("access log should contain first status: %q", output.String())
	}
}

func TestAccessLog_RecordsErrorStatuses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "not found", statusCode: http.StatusNotFound},
		{name: "method not allowed", statusCode: http.StatusMethodNotAllowed},
		{name: "internal server error", statusCode: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, http.StatusText(tt.statusCode), tt.statusCode)
			}), log.New(&output, "", 0))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/error", nil))

			if got := recorder.Code; got != tt.statusCode {
				t.Fatalf("status=%d want=%d", got, tt.statusCode)
			}
			if want := "| " + http.StatusText(tt.statusCode); strings.Contains(output.String(), want) {
				t.Fatalf("access log should record numeric status only: %q", output.String())
			}
			statusPattern := regexp.MustCompile(`\|\s+` + strconv.Itoa(tt.statusCode) + `\s+\|`)
			if !statusPattern.MatchString(output.String()) {
				t.Fatalf("access log should contain status %d: %q", tt.statusCode, output.String())
			}
		})
	}
}

func TestAccessLog_PreservesFlusher(t *testing.T) {
	var output bytes.Buffer
	flusherExposed := false
	handler := accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusherExposed = true
		flusher.Flush()
	}), log.New(&output, "", 0))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if !flusherExposed {
		t.Fatal("wrapped response writer should expose http.Flusher")
	}
	if !recorder.Flushed {
		t.Fatal("wrapped response writer should flush the underlying writer")
	}
	if !strings.Contains(output.String(), "| 200 |") {
		t.Fatalf("flush should record implicit status 200: %q", output.String())
	}
}
