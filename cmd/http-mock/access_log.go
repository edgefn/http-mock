package main

import (
	"log"
	"net"
	"net/http"
	"time"
)

func accessLog(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		responseWriter := &statusResponseWriter{ResponseWriter: w}
		wrappedWriter := http.ResponseWriter(responseWriter)
		if flusher, ok := w.(http.Flusher); ok {
			wrappedWriter = &flushingResponseWriter{
				statusResponseWriter: responseWriter,
				flusher:              flusher,
			}
		}

		defer func() {
			logger.Printf(
				"[HTTP-MOCK] | %3d | %13v | %15s | %-7s %q",
				responseWriter.statusCode(),
				time.Since(startedAt),
				clientIP(r.RemoteAddr),
				r.Method,
				r.URL.RequestURI(),
			)
		}()

		next.ServeHTTP(wrappedWriter, r)
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	if w.status == 0 {
		w.status = statusCode
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

type flushingResponseWriter struct {
	*statusResponseWriter
	flusher http.Flusher
}

func (w *flushingResponseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.flusher.Flush()
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
