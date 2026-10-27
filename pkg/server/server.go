package server

import (
	"bytes"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"log"
	"math/rand"
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
	routesSources routes.Sources

	mu     sync.RWMutex
	routes []routes.Route
}

func Load(dataRoot string, routesPath string) (*Server, error) {
	cfg, sources, err := routes.Load(dataRoot, routesPath)
	if err != nil {
		return nil, err
	}
	return &Server{
		dataRoot:      dataRoot,
		routesPath:    routesPath,
		routesSources: sources,
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
		if err := serveRoute(w, r, route); err != nil {
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
	sources, err := routes.DiscoverSources(s.dataRoot, s.routesPath)
	if err != nil {
		log.Printf("http-mock reload routes discovery failed pattern=%q: %v", s.routesPath, err)
		return
	}

	s.mu.RLock()
	loadedFingerprint := s.routesSources.Fingerprint
	unchanged := sources.Fingerprint == loadedFingerprint
	s.mu.RUnlock()
	if unchanged {
		return
	}

	cfg, loadedSources, err := routes.Load(s.dataRoot, s.routesPath)
	if err != nil {
		log.Printf("http-mock reload routes failed pattern=%q: %v", s.routesPath, err)
		return
	}

	s.mu.Lock()
	if s.routesSources.Fingerprint != loadedFingerprint {
		s.mu.Unlock()
		return
	}
	s.routesSources = loadedSources
	s.routes = cfg.Routes
	s.mu.Unlock()
	log.Printf("http-mock reloaded routes pattern=%q files=%d", s.routesPath, len(loadedSources.Files))
}

func (s *Server) routesSnapshot() []routes.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.routes
}

func serveRoute(w http.ResponseWriter, r *http.Request, route routes.Route) error {
	if ok := waitBeforeResponse(r, route); !ok {
		return nil
	}

	body, err := responseBody(route)
	if err != nil {
		return err
	}
	body = expandBodyVars(body, r)
	if route.ContentType != "" {
		w.Header().Set("Content-Type", route.ContentType)
	}
	for name, value := range route.Headers {
		w.Header().Set(name, value)
	}
	if route.StreamDelayDuration() <= 0 || !IsStreamContentType(route.ContentType) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	}
	w.WriteHeader(route.StatusCode)
	if route.StreamDelayDuration() > 0 && IsStreamContentType(route.ContentType) {
		return writeStreamBody(w, r, body, route.StreamDelayDuration())
	}
	_, err = w.Write(body)
	return err
}

func waitBeforeResponse(r *http.Request, route routes.Route) bool {
	delay := route.DelayDuration() + randomDelay(route)
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

func randomDelay(route routes.Route) time.Duration {
	min, max, ok := route.RandomDelayRange()
	if !ok {
		return 0
	}
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int63n(int64(max-min)+1))
}

func responseBody(route routes.Route) ([]byte, error) {
	if body, ok := route.InlineBody(); ok {
		return []byte(body), nil
	}
	return os.ReadFile(route.ResponsePath())
}

// expandBodyVars replaces template placeholders in body:
//   - {{uuid}}: a fresh UUID v4 on each call
//   - {{path_last}}: the last path segment of the request URL (e.g. the task ID
//     in /api/v3/.../tasks/{id}), enabling GET fixtures to echo back the ID
//     that the caller received from a prior create response
func expandBodyVars(body []byte, r *http.Request) []byte {
	if bytes.Contains(body, []byte("{{uuid}}")) {
		body = bytes.ReplaceAll(body, []byte("{{uuid}}"), []byte(newUUID()))
	}
	if bytes.Contains(body, []byte("{{path_last}}")) {
		path := strings.TrimRight(r.URL.Path, "/")
		last := path[strings.LastIndexByte(path, '/')+1:]
		body = bytes.ReplaceAll(body, []byte("{{path_last}}"), []byte(last))
	}
	return body
}

func newUUID() string {
	var b [16]byte
	_, _ = cryptorand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeStreamBody(w http.ResponseWriter, r *http.Request, body []byte, delay time.Duration) error {
	flusher, _ := w.(http.Flusher)
	chunks := bytes.SplitAfter(body, []byte("\n\n"))
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		if i > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-r.Context().Done():
				timer.Stop()
				return nil
			}
			timer.Stop()
		}
		if _, err := w.Write(chunk); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return nil
}

func IsStreamContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}
