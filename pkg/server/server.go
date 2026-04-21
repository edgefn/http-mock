package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/edgefn/http-mock/pkg/routes"
)

type Server struct {
	routes []routes.Route
}

func Load(dataRoot string, routesPath string) (*Server, error) {
	cfg, _, err := routes.Load(dataRoot, routesPath)
	if err != nil {
		return nil, err
	}
	return &Server{routes: cfg.Routes}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read request body: %v", err), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	var pathMatched bool
	var methodMatched bool
	for _, route := range s.routes {
		if !route.MatchesPath(r.URL.Path) {
			continue
		}
		pathMatched = true
		if route.Method != r.Method {
			continue
		}
		methodMatched = true
		if !route.Allows(r, body) {
			continue
		}
		if err := serveRoute(w, route); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if pathMatched && !methodMatched {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if pathMatched {
		http.Error(w, "mock route not matched", http.StatusNotFound)
		return
	}
	http.Error(w, "mock route not found", http.StatusNotFound)
}

func serveRoute(w http.ResponseWriter, route routes.Route) error {
	body, err := os.ReadFile(route.ResponsePath())
	if err != nil {
		return err
	}
	if route.ContentType != "" {
		w.Header().Set("Content-Type", route.ContentType)
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(route.StatusCode)
	_, err = w.Write(body)
	return err
}

func IsStreamContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}
