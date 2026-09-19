package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// UnmarshalJSON selects the config by type while preserving the existing flat
// JSON shape. Each decoder is strict: even a zero-valued field for the other
// check type is rejected, rather than silently dropped during conversion.
func (s *Step) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateStepFields(data); err != nil {
		return err
	}
	var wire struct {
		ID     string          `json:"id"`
		Name   string          `json:"name"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	step := Step{ID: wire.ID, Name: wire.Name, Type: wire.Type}
	switch wire.Type {
	case "http.check":
		var config struct {
			URL            string `json:"url"`
			ExpectedStatus int    `json:"expectedStatus"`
			TimeoutMS      int    `json:"timeoutMs"`
		}
		if err := decodeStrict(wire.Config, &config); err != nil {
			return fmt.Errorf("step %q HTTP config: %w", wire.ID, err)
		}
		step.Config = CheckConfig{URL: config.URL, ExpectedStatus: config.ExpectedStatus, TimeoutMS: config.TimeoutMS}
	case "tcp.check":
		var config struct {
			Host      string `json:"host"`
			Port      int    `json:"port"`
			TimeoutMS int    `json:"timeoutMs"`
		}
		if err := decodeStrict(wire.Config, &config); err != nil {
			return fmt.Errorf("step %q TCP config: %w", wire.ID, err)
		}
		step.Config = CheckConfig{Host: config.Host, Port: config.Port, TimeoutMS: config.TimeoutMS}
	default:
		return fmt.Errorf("step %q type must be http.check or tcp.check", wire.ID)
	}
	*s = step
	return nil
}

// Inspect only the step object's keys. Raw config values are decoded later;
// accepting a second config here could hide unknown fields in the first one.
func rejectDuplicateStepFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return fmt.Errorf("step must be a JSON object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("step field name must be a string")
		}
		// encoding/json accepts case-insensitive matches for these ASCII names.
		name := strings.ToLower(key)
		if seen[name] {
			return fmt.Errorf("duplicate step field %q", key)
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	_, err = decoder.Token() // Consume the closing object delimiter.
	return err
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON document")
	}
	return nil
}
