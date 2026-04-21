package server

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/edgefn/http-mock/pkg/routes"
)

type Server struct {
	dataRoot      string
	routesPath    string
	routesFile    string
	routesModTime time.Time

	mu     sync.RWMutex
	routes []routes.Route
}

func Load(dataRoot string, routesPath string) (*Server, error) {
	cfg, routesFile, err := routes.Load(dataRoot, routesPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(routesFile)
	if err != nil {
		return nil, err
	}
	return &Server{
		dataRoot:      dataRoot,
		routesPath:    routesPath,
		routesFile:    routesFile,
		routesModTime: info.ModTime(),
		routes:        cfg.Routes,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.reloadRoutesIfChanged()
	routesSnapshot := s.routesSnapshot()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read request body: %v", err), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	var pathMatched bool
	var methodMatched bool
	for _, route := range routesSnapshot {
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

func (s *Server) reloadRoutesIfChanged() {
	info, err := os.Stat(s.routesFile)
	if err != nil {
		log.Printf("http-mock reload routes stat failed path=%q: %v", s.routesFile, err)
		return
	}

	s.mu.RLock()
	loadedModTime := s.routesModTime
	unchanged := info.ModTime().Equal(loadedModTime)
	s.mu.RUnlock()
	if unchanged {
		return
	}

	cfg, routesFile, err := routes.Load(s.dataRoot, s.routesPath)
	if err != nil {
		log.Printf("http-mock reload routes failed path=%q: %v", s.routesFile, err)
		return
	}

	s.mu.Lock()
	if !s.routesModTime.Equal(loadedModTime) {
		s.mu.Unlock()
		return
	}
	s.routesModTime = info.ModTime()
	s.routes = cfg.Routes
	s.mu.Unlock()
	log.Printf("http-mock reloaded routes path=%q", routesFile)
}

func (s *Server) routesSnapshot() []routes.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.routes
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
