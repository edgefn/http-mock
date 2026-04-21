package routes

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Routes []Route `yaml:"routes"`
}

type Route struct {
	Path         string            `yaml:"path"`
	Method       string            `yaml:"method"`
	ResponseFile string            `yaml:"response_file"`
	BodyInline   *string           `yaml:"body_inline"`
	ContentType  string            `yaml:"content_type"`
	StatusCode   int               `yaml:"status_code"`
	Headers      map[string]string `yaml:"headers"`
	Delay        string            `yaml:"delay"`
	RandomDelay  *RandomDelay      `yaml:"random_delay"`
	StreamDelay  string            `yaml:"stream_delay"`
	Match        *Match            `yaml:"match"`

	responsePath        string
	delayDuration       time.Duration
	streamDelayDuration time.Duration
}

type Match struct {
	Header   string `yaml:"header"`
	Query    string `yaml:"query"`
	JSONPath string `yaml:"json_path"`
	Equals   string `yaml:"equals"`
}

type RandomDelay struct {
	Min string `yaml:"min"`
	Max string `yaml:"max"`

	minDuration time.Duration
	maxDuration time.Duration
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
	hasResponseFile := strings.TrimSpace(r.ResponseFile) != ""
	hasInlineBody := r.BodyInline != nil
	if !hasResponseFile && !hasInlineBody {
		return fmt.Errorf("routes[%d].response_file or body_inline is required", index)
	}
	if hasResponseFile && hasInlineBody {
		return fmt.Errorf("routes[%d].response_file and body_inline must not both be set", index)
	}
	if r.StatusCode == 0 {
		r.StatusCode = http.StatusOK
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	r.Method = method

	if hasResponseFile {
		r.responsePath = resolvePath(dataRoot, r.ResponseFile)
		if _, err := os.Stat(r.responsePath); err != nil {
			return fmt.Errorf("routes[%d].response_file: %w", index, err)
		}
	}
	if strings.TrimSpace(r.ContentType) == "" {
		if hasResponseFile {
			r.ContentType = guessContentType(r.ResponseFile)
		} else {
			r.ContentType = "text/plain; charset=utf-8"
		}
	}
	if err := r.normalizeHeaders(index); err != nil {
		return err
	}
	delay, err := parseOptionalDuration(r.Delay, index, "delay")
	if err != nil {
		return err
	}
	r.delayDuration = delay
	streamDelay, err := parseOptionalDuration(r.StreamDelay, index, "stream_delay")
	if err != nil {
		return err
	}
	r.streamDelayDuration = streamDelay
	if r.RandomDelay != nil {
		if err := r.RandomDelay.validate(index); err != nil {
			return err
		}
	}
	if r.Match != nil {
		if err := r.Match.validate(index); err != nil {
			return err
		}
	}
	return nil
}

func (r *Route) normalizeHeaders(index int) error {
	if len(r.Headers) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(r.Headers))
	for key, value := range r.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("routes[%d].headers must not contain empty header names", index)
		}
		normalized[http.CanonicalHeaderKey(key)] = value
	}
	r.Headers = normalized
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

func (d *RandomDelay) validate(index int) error {
	min, err := parseRequiredDuration(d.Min, index, "random_delay.min")
	if err != nil {
		return err
	}
	max, err := parseRequiredDuration(d.Max, index, "random_delay.max")
	if err != nil {
		return err
	}
	if max < min {
		return fmt.Errorf("routes[%d].random_delay.max must be greater than or equal to random_delay.min", index)
	}
	d.minDuration = min
	d.maxDuration = max
	return nil
}

func parseOptionalDuration(value string, index int, field string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return parseDuration(value, index, field)
}

func parseRequiredDuration(value string, index int, field string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("routes[%d].%s is required", index, field)
	}
	return parseDuration(value, index, field)
}

func parseDuration(value string, index int, field string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("routes[%d].%s must be a valid duration: %w", index, field, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("routes[%d].%s must be >= 0", index, field)
	}
	return duration, nil
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

func (r Route) InlineBody() (string, bool) {
	if r.BodyInline == nil {
		return "", false
	}
	return *r.BodyInline, true
}

func (r Route) DelayDuration() time.Duration {
	return r.delayDuration
}

func (r Route) StreamDelayDuration() time.Duration {
	return r.streamDelayDuration
}

func (r Route) RandomDelayRange() (time.Duration, time.Duration, bool) {
	if r.RandomDelay == nil {
		return 0, 0, false
	}
	return r.RandomDelay.minDuration, r.RandomDelay.maxDuration, true
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
