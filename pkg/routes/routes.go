package routes

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Routes []Route `yaml:"routes"`
}

type Route struct {
	Path         string `yaml:"path"`
	Method       string `yaml:"method"`
	ResponseFile string `yaml:"response_file"`
	ContentType  string `yaml:"content_type"`
	StatusCode   int    `yaml:"status_code"`
	Match        *Match `yaml:"match"`

	responsePath string
}

type Match struct {
	Header   string `yaml:"header"`
	Query    string `yaml:"query"`
	JSONPath string `yaml:"json_path"`
	Equals   string `yaml:"equals"`
}

func Load(dataRoot string, routesPath string) (*Config, string, error) {
	resolvedPath := resolvePath(dataRoot, routesPath)
	b, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, "", err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, "", err
	}
	if err := cfg.Validate(dataRoot); err != nil {
		return nil, "", err
	}
	return &cfg, resolvedPath, nil
}

func (c *Config) Validate(dataRoot string) error {
	if c == nil {
		return fmt.Errorf("routes config is nil")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("routes must not be empty")
	}
	for i := range c.Routes {
		if err := c.Routes[i].validate(i, dataRoot); err != nil {
			return err
		}
	}
	return nil
}

func (r *Route) validate(index int, dataRoot string) error {
	if strings.TrimSpace(r.Path) == "" || !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("routes[%d].path must start with '/'", index)
	}
	if strings.TrimSpace(r.ResponseFile) == "" {
		return fmt.Errorf("routes[%d].response_file is required", index)
	}
	if r.StatusCode == 0 {
		r.StatusCode = http.StatusOK
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	r.Method = method

	r.responsePath = resolvePath(dataRoot, r.ResponseFile)
	if _, err := os.Stat(r.responsePath); err != nil {
		return fmt.Errorf("routes[%d].response_file: %w", index, err)
	}
	if strings.TrimSpace(r.ContentType) == "" {
		r.ContentType = guessContentType(r.ResponseFile)
	}
	if r.Match != nil {
		if err := r.Match.validate(index); err != nil {
			return err
		}
	}
	return nil
}

func (m *Match) validate(index int) error {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(m.Header) == "" && strings.TrimSpace(m.Query) == "" && strings.TrimSpace(m.JSONPath) == "" {
		return fmt.Errorf("routes[%d].match must define header, query, or json_path", index)
	}
	if strings.TrimSpace(m.Equals) == "" {
		return fmt.Errorf("routes[%d].match.equals is required", index)
	}
	return nil
}

func (r *Route) Allows(req *http.Request, body []byte) bool {
	if !r.MatchesPath(req.URL.Path) {
		return false
	}
	if req.Method != r.Method {
		return false
	}
	if r.Match == nil {
		return true
	}
	return r.Match.Matches(req, body)
}

func (r Route) MatchesPath(path string) bool {
	return matchPathPattern(r.Path, path)
}

func (m *Match) Matches(req *http.Request, body []byte) bool {
	if m == nil {
		return true
	}
	var value string
	if header := strings.TrimSpace(m.Header); header != "" {
		value = req.Header.Get(header)
	} else if query := strings.TrimSpace(m.Query); query != "" {
		value = req.URL.Query().Get(query)
	} else {
		result := gjson.GetBytes(body, m.JSONPath)
		if !result.Exists() {
			value = ""
		} else if result.Type == gjson.JSON {
			value = result.Raw
		} else {
			value = result.String()
		}
	}
	return value == m.Equals
}

func matchPathPattern(pattern string, path string) bool {
	if !strings.Contains(pattern, "{") {
		return pattern == path
	}

	patternSegments := strings.Split(pattern, "/")
	pathSegments := strings.Split(path, "/")
	if len(patternSegments) != len(pathSegments) {
		return false
	}
	for i := range patternSegments {
		if !matchPathSegment(patternSegments[i], pathSegments[i]) {
			return false
		}
	}
	return true
}

func matchPathSegment(pattern string, value string) bool {
	for pattern != "" {
		open := strings.IndexByte(pattern, '{')
		if open < 0 {
			return pattern == value
		}
		if open > 0 {
			literal := pattern[:open]
			if !strings.HasPrefix(value, literal) {
				return false
			}
			pattern = pattern[open:]
			value = value[len(literal):]
			continue
		}

		close := strings.IndexByte(pattern, '}')
		if close <= 1 {
			return false
		}
		pattern = pattern[close+1:]
		if pattern == "" {
			return value != ""
		}
		nextOpen := strings.IndexByte(pattern, '{')
		if nextOpen == 0 {
			return false
		}
		literal := pattern
		if nextOpen > 0 {
			literal = pattern[:nextOpen]
		}
		next := strings.Index(value, literal)
		if next <= 0 {
			return false
		}
		value = value[next:]
	}
	return value == ""
}

func (r *Route) ResponsePath() string {
	return r.responsePath
}

func guessContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "application/json"
	case ".sse":
		return "text/event-stream"
	case ".mp3":
		return "audio/mpeg"
	default:
		return "application/octet-stream"
	}
}

func resolvePath(dataRoot string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dataRoot, path)
}
