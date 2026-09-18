package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func validDefinition() Definition {
	return Definition{SchemaVersion: 1, ID: "example", Name: "Example", Steps: []Step{
		{ID: "check", Name: "Check", Type: "http.check", Config: HTTPConfig{URL: "http://localhost/health", ExpectedStatus: 200, TimeoutMS: 1000}},
	}}
}

func TestLoadDefinitionSizeLimit(t *testing.T) {
	want := validDefinition()
	want.Description = "Service status — café" // The limit counts bytes, not characters.
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		size int
	}{
		{"ordinary file", len(encoded)},
		{"one byte below limit", maxDefinitionBytes - 1},
		{"exact limit", maxDefinitionBytes},
		{"one byte over limit", maxDefinitionBytes + 1},
		{"much larger file", 16 * maxDefinitionBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "workflow.json")
			// Trailing JSON whitespace makes every fixture valid apart from its size.
			// The one-byte-over case also catches silently accepting a limited prefix.
			content := string(encoded) + strings.Repeat(" ", test.size-len(encoded))
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			definitions, err := Load(directory)
			if test.size > maxDefinitionBytes {
				if err == nil || err.Error() != path+": definition exceeds 64 KiB" || definitions != nil {
					t.Fatalf("expected size rejection with the file path, got definitions=%+v error=%v", definitions, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(definitions, []Definition{want}) {
				t.Fatalf("valid %d-byte workflow did not load unchanged: definitions=%+v error=%v", test.size, definitions, err)
			}
		})
	}
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

func TestLoadJSONAndRejectBadFiles(t *testing.T) {
	dir := t.TempDir()
	want := validDefinition()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workflow.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	definitions, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(definitions, []Definition{want}) {
		t.Fatalf("loaded definitions differ from the file: %+v", definitions)
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
		if _, err := Load(dir); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}
