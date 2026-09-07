package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validDefinition() Definition {
	return Definition{SchemaVersion: 1, ID: "example", Name: "Example", Steps: []Step{
		{ID: "check", Name: "Check", Type: "http.check", Config: HTTPConfig{URL: "http://localhost/health", ExpectedStatus: 200, TimeoutMS: 1000}},
	}}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Definition)
	}{
		{"unknown version", func(d *Definition) { d.SchemaVersion = 2 }},
		{"empty workflow", func(d *Definition) { d.Steps = nil }},
		{"duplicate step", func(d *Definition) { d.Steps = append(d.Steps, d.Steps[0]) }},
		{"unsupported node", func(d *Definition) { d.Steps[0].Type = "ssh.command" }},
		{"relative URL", func(d *Definition) { d.Steps[0].Config.URL = "/health" }},
		{"non HTTP URL", func(d *Definition) { d.Steps[0].Config.URL = "file:///etc/passwd" }},
		{"credentials in URL", func(d *Definition) { d.Steps[0].Config.URL = "https://user:pass@example.com" }},
		{"unbounded timeout", func(d *Definition) { d.Steps[0].Config.TimeoutMS = 0 }},
		{"invalid status", func(d *Definition) { d.Steps[0].Config.ExpectedStatus = 999 }},
	}
	if err := Validate(validDefinition()); err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := validDefinition()
			test.edit(&d)
			if Validate(d) == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
}

func TestLoadExamplesAndRejectBadFiles(t *testing.T) {
	definitions, err := Load("../../examples", "http://localhost:9999")
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 4 {
		t.Fatalf("got %d examples", len(definitions))
	}
	for _, d := range definitions {
		for _, step := range d.Steps {
			if !strings.HasPrefix(step.Config.URL, "http://localhost:9999/") {
				t.Fatal("demo URL was not resolved")
			}
		}
	}
	tests := []string{
		`{"schemaVersion":1,"unknown":true}`,
		`{"schemaVersion":1} {"id":"second-document"}`,
		`{"schemaVersion":1,"id":"empty","name":"Empty","steps":[]}`,
		`this is not JSON`,
	}
	for _, content := range tests {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir, "http://localhost"); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}
