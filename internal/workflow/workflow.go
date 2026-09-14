// Package workflow defines the small, sequential workflow format used in M0.
// It contains data and validation, with no HTTP calls or background execution.
package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Definition struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Steps         []Step `json:"steps"`
}

type Step struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Type   string     `json:"type"`
	Config HTTPConfig `json:"config"`
}

// HTTPConfig is intentionally concrete: M0 has exactly one node type.
type HTTPConfig struct {
	URL            string `json:"url"`
	ExpectedStatus int    `json:"expectedStatus"`
	TimeoutMS      int    `json:"timeoutMs"`
}

type HTTPResult struct {
	Healthy        bool   `json:"healthy"`
	URL            string `json:"url"`
	ExpectedStatus int    `json:"expectedStatus"`
	StatusCode     int    `json:"statusCode,omitempty"`
	DurationMS     int64  `json:"durationMs"`
	Reason         string `json:"reason"`
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func Validate(d Definition) error {
	if d.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1")
	}
	if !identifier.MatchString(d.ID) || strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("workflow needs a name and an id of 1–64 lowercase letters, numbers, underscores or hyphens")
	}
	if len(d.Steps) == 0 || len(d.Steps) > 20 {
		return fmt.Errorf("workflow must contain 1–20 steps")
	}
	seen := make(map[string]bool)
	for _, step := range d.Steps {
		if !identifier.MatchString(step.ID) || seen[step.ID] {
			return fmt.Errorf("step id %q is invalid or duplicated", step.ID)
		}
		seen[step.ID] = true
		if strings.TrimSpace(step.Name) == "" || step.Type != "http.check" {
			return fmt.Errorf("step %q needs a name and type http.check", step.ID)
		}
		u, err := url.Parse(step.Config.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return fmt.Errorf("step %q needs an absolute HTTP(S) URL without credentials or a fragment", step.ID)
		}
		if step.Config.ExpectedStatus < 100 || step.Config.ExpectedStatus > 599 {
			return fmt.Errorf("step %q expectedStatus must be between 100 and 599", step.ID)
		}
		if step.Config.TimeoutMS < 100 || step.Config.TimeoutMS > 30000 {
			return fmt.Errorf("step %q timeoutMs must be between 100 and 30000", step.ID)
		}
	}
	return nil
}

// Load reads and validates workflow JSON files once at startup.
func Load(directory string) ([]Definition, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read workflows: %w", err)
	}
	definitions := make([]Definition, 0)
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(data) > 64*1024 {
			return nil, fmt.Errorf("%s: definition exceeds 64 KiB", path)
		}
		var d Definition
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&d); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("%s: expected exactly one JSON document", path)
		}
		if err := Validate(d); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("%s: duplicate workflow id %q", path, d.ID)
		}
		seen[d.ID] = true
		definitions = append(definitions, d)
	}
	if len(definitions) == 0 {
		return nil, fmt.Errorf("no workflow JSON files found in %s", directory)
	}
	return definitions, nil
}
