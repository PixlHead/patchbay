package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateTCP(t *testing.T) {
	definition := validDefinition()
	definition.Steps = append(definition.Steps, Step{
		ID: "port", Name: "Port", Type: "tcp.check",
		Config: CheckConfig{Host: "nas.home", Port: 22, TimeoutMS: 1000},
	})
	for _, host := range []string{"nas", "NAS.home", "nas.home.", "192.0.2.10", "::1", "2001:db8::1", "fe80::1%eth0"} {
		t.Run(host, func(t *testing.T) {
			definition.Steps[1].Config.Host = host
			if err := Validate(definition); err != nil {
				t.Fatalf("valid TCP host rejected: %v", err)
			}
		})
	}
	for _, test := range []struct {
		name string
		edit func(*CheckConfig)
	}{
		{"missing host", func(c *CheckConfig) { c.Host = "" }},
		{"URL", func(c *CheckConfig) { c.Host = "https://nas.home" }},
		{"port in host", func(c *CheckConfig) { c.Host = "nas.home:22" }},
		{"bracketed IPv6", func(c *CheckConfig) { c.Host = "[::1]" }},
		{"credentials", func(c *CheckConfig) { c.Host = "user@nas.home" }},
		{"path", func(c *CheckConfig) { c.Host = "nas.home/health" }},
		{"whitespace", func(c *CheckConfig) { c.Host = " nas.home" }},
		{"zone whitespace", func(c *CheckConfig) { c.Host = "fe80::1%bad zone" }},
		{"empty DNS label", func(c *CheckConfig) { c.Host = "nas..home" }},
		{"invalid DNS label", func(c *CheckConfig) { c.Host = "-nas.home" }},
		{"long DNS label", func(c *CheckConfig) { c.Host = strings.Repeat("a", 64) }},
		{"long hostname", func(c *CheckConfig) { c.Host = strings.Repeat("a.", 127) + "a" }},
		{"missing port", func(c *CheckConfig) { c.Port = 0 }},
		{"negative port", func(c *CheckConfig) { c.Port = -1 }},
		{"large port", func(c *CheckConfig) { c.Port = 65536 }},
		{"short timeout", func(c *CheckConfig) { c.TimeoutMS = 99 }},
		{"long timeout", func(c *CheckConfig) { c.TimeoutMS = 30001 }},
		{"HTTP URL field", func(c *CheckConfig) { c.URL = "http://nas.home" }},
		{"HTTP status field", func(c *CheckConfig) { c.ExpectedStatus = 200 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition.Steps[1].Config = CheckConfig{Host: "nas.home", Port: 22, TimeoutMS: 1000}
			test.edit(&definition.Steps[1].Config)
			if err := Validate(definition); err == nil {
				t.Fatal("invalid TCP config accepted")
			}
		})
	}
	for _, port := range []int{1, 65535} {
		for _, timeout := range []int{100, 30000} {
			definition.Steps[1].Config = CheckConfig{Host: "nas", Port: port, TimeoutMS: timeout}
			if err := Validate(definition); err != nil {
				t.Fatalf("valid TCP limits rejected: %v", err)
			}
		}
	}
	definition = validDefinition()
	definition.Steps[0].Config.Host = "nas"
	if err := Validate(definition); err == nil {
		t.Fatal("HTTP accepted a TCP field supplied programmatically")
	}
}

func TestLoadMixedChecksAndPreserveJSON(t *testing.T) {
	const content = `{"schemaVersion":1,"id":"mixed","name":"Mixed checks","description":"",
		"steps":[
		{"id":"http","name":"HTTP","type":"http.check","config":{"url":"http://nas/health","expectedStatus":200,"timeoutMs":1000}},
		{"id":"tcp","name":"TCP","type":"tcp.check","config":{"host":"::1","port":22,"timeoutMs":1000}}
		]}`
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "checks.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	definitions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || len(definitions[0].Steps) != 2 || definitions[0].Steps[1].Config.Host != "::1" {
		t.Fatalf("mixed checks did not load: %+v", definitions)
	}
	encoded, err := json.Marshal(definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal([]byte(content), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("check JSON changed shape: %s", encoded)
	}
}

func TestStepJSONRejectsWrongFields(t *testing.T) {
	for _, content := range []string{
		`{"id":"check","name":"Check","type":"http.check","extra":true,"config":{"url":"http://nas","expectedStatus":200,"timeoutMs":1000}}`,
		`{"id":"check","name":"Check","type":"http.check","config":{"url":"http://nas","expectedStatus":200,"timeoutMs":1000,"host":""}}`,
		`{"id":"check","name":"Check","type":"http.check","config":{"url":"http://nas","expectedStatus":200,"timeoutMs":1000,"port":0}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"host":"nas","port":22,"timeoutMs":1000,"url":""}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"host":"nas","port":22,"timeoutMs":1000,"expectedStatus":0}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"host":"nas","port":22,"timeoutMs":1000,"extra":true}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"host":"nas","port":"22","timeoutMs":1000}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"host":"nas","port":22.5,"timeoutMs":1000}}`,
		`{"id":"check","name":"Check","type":"ssh.command","config":{}}`,
	} {
		original := validDefinition().Steps[0]
		got := original
		if err := json.Unmarshal([]byte(content), &got); err == nil {
			t.Fatalf("accepted invalid step: %s", content)
		}
		if got != original {
			t.Fatal("failed decoding changed the previous step value")
		}
	}
}

func TestStepJSONRejectsDuplicateMembers(t *testing.T) {
	for _, content := range []string{
		`{"id":"check","name":"Check","type":"tcp.check","config":{"unexpected":true},"config":{"host":"nas","port":22,"timeoutMs":1000}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"host":"nas","port":22,"timeoutMs":1000},"config":{"unexpected":true}}`,
		`{"id":"check","name":"Check","type":"tcp.check","config":{"unexpected":true},"Config":{"host":"nas","port":22,"timeoutMs":1000}}`,
		`{"id":"check","name":"Check","type":"http.check","type":"tcp.check","config":{"host":"nas","port":22,"timeoutMs":1000}}`,
		`{"id":"first","ID":"second","name":"Check","type":"tcp.check","config":{"host":"nas","port":22,"timeoutMs":1000}}`,
	} {
		original := validDefinition().Steps[0]
		got := original
		err := json.Unmarshal([]byte(content), &got)
		if err == nil || !strings.Contains(err.Error(), "duplicate step field") {
			t.Fatalf("expected duplicate step member rejection, got %v for %s", err, content)
		}
		if got != original {
			t.Fatal("duplicate step member changed the previous step value")
		}
	}
}
