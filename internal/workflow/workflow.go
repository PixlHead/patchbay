// Package workflow defines the small, sequential workflow format.
// It contains data and validation, with no network calls or background execution.
package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const maxDefinitionBytes = 64 * 1024

type Definition struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Steps         []Step `json:"steps"`
}

type Step struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Config CheckConfig `json:"config"`
}

// CheckConfig holds the two supported check configurations as plain values.
// HTTP uses URL/ExpectedStatus; TCP uses Host/Port. Both use TimeoutMS.
// Step.UnmarshalJSON and Validate reject fields belonging to the other check type.
type CheckConfig struct {
	URL            string `json:"url,omitempty"`
	ExpectedStatus int    `json:"expectedStatus,omitempty"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	TimeoutMS      int    `json:"timeoutMs"`
}

// CheckResult is copied with each run snapshot. An absent Type means HTTP,
// preserving the original HTTP JSON format; TCP results identify themselves.
type CheckResult struct {
	Type           string `json:"type,omitempty"`
	Healthy        bool   `json:"healthy"`
	URL            string `json:"url,omitempty"`
	ExpectedStatus int    `json:"expectedStatus,omitempty"`
	StatusCode     int    `json:"statusCode,omitempty"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
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
		if strings.TrimSpace(step.Name) == "" {
			return fmt.Errorf("step %q needs a name", step.ID)
		}
		switch step.Type {
		case "http.check":
			if step.Config.Host != "" || step.Config.Port != 0 {
				return fmt.Errorf("step %q HTTP config cannot contain host or port", step.ID)
			}
			u, err := url.Parse(step.Config.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
				return fmt.Errorf("step %q needs an absolute HTTP(S) URL without credentials or a fragment", step.ID)
			}
			if step.Config.ExpectedStatus < 100 || step.Config.ExpectedStatus > 599 {
				return fmt.Errorf("step %q expectedStatus must be between 100 and 599", step.ID)
			}
		case "tcp.check":
			if step.Config.URL != "" || step.Config.ExpectedStatus != 0 {
				return fmt.Errorf("step %q TCP config cannot contain url or expectedStatus", step.ID)
			}
			if !validTCPHost(step.Config.Host) {
				return fmt.Errorf("step %q host must be a DNS name or IP address without a URL, brackets, or port", step.ID)
			}
			if step.Config.Port < 1 || step.Config.Port > 65535 {
				return fmt.Errorf("step %q port must be between 1 and 65535", step.ID)
			}
		default:
			return fmt.Errorf("step %q type must be http.check or tcp.check", step.ID)
		}
		if step.Config.TimeoutMS < 100 || step.Config.TimeoutMS > 30000 {
			return fmt.Errorf("step %q timeoutMs must be between 100 and 30000", step.ID)
		}
	}
	return nil
}

func validTCPHost(host string) bool {
	if strings.ContainsAny(host, "/\\[]@?#") || strings.ContainsFunc(host, func(c rune) bool {
		return unicode.IsSpace(c) || unicode.IsControl(c)
	}) {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true // Includes unbracketed IPv6 and optional interface zones.
	}
	// DNS names may be a single local name or end with the absolute-name dot.
	name := strings.TrimSuffix(host, ".")
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
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
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		// One extra byte detects oversized files without reading them in full.
		data, err := io.ReadAll(io.LimitReader(file, maxDefinitionBytes+1))
		file.Close() // Close each file now, rather than deferring across the loop.
		if err != nil {
			return nil, err
		}
		if len(data) > maxDefinitionBytes {
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
