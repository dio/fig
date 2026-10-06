// Package bundle decodes configuration envelopes and resolves exact references.
// It deliberately knows nothing about Match, WAF, Envoy or pipeline execution.
package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const MaxBytes = 1 << 20

type Ref struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
}
type Resource struct {
	Ref
	Spec json.RawMessage `json:"spec"`
}
type wire struct {
	APIVersion string     `json:"apiVersion"`
	Scope      string     `json:"scope"`
	Generation string     `json:"generation"`
	Resources  []Resource `json:"resources"`
}
type Bundle struct {
	scope, generation string
	resources         map[Ref]json.RawMessage
}

func Decode(data []byte) (*Bundle, error) {
	var w wire
	if err := DecodeSpec(data, &w); err != nil {
		return nil, err
	}
	if w.APIVersion != "fig/v1alpha1" || w.Scope == "" || w.Generation == "" || len(w.Resources) == 0 {
		return nil, fmt.Errorf("bundle requires apiVersion, scope, generation and resources")
	}
	b := &Bundle{scope: w.Scope, generation: w.Generation, resources: map[Ref]json.RawMessage{}}
	identities := map[[2]string]bool{}
	for _, r := range w.Resources {
		if r.Type == "" || r.Name == "" || r.Version == "" || len(r.Spec) == 0 {
			return nil, fmt.Errorf("incomplete resource")
		}
		key := [2]string{r.Type, r.Name}
		if identities[key] {
			return nil, fmt.Errorf("duplicate resource %s/%s", r.Type, r.Name)
		}
		identities[key] = true
		b.resources[r.Ref] = bytes.Clone(r.Spec)
	}
	return b, nil
}
func (b *Bundle) Scope() string      { return b.scope }
func (b *Bundle) Generation() string { return b.generation }

// Resources returns caller-owned copies, never mutable prepared state.
func (b *Bundle) Resources() []Resource {
	out := []Resource{}
	for ref, spec := range b.resources {
		out = append(out, Resource{Ref: ref, Spec: bytes.Clone(spec)})
	}
	return out
}
func (b *Bundle) Resolve(ref Ref) (json.RawMessage, error) {
	spec, ok := b.resources[ref]
	if !ok {
		return nil, fmt.Errorf("unresolved reference %s/%s@%s", ref.Type, ref.Name, ref.Version)
	}
	return bytes.Clone(spec), nil
}

// DecodeSpec rejects unknown fields, duplicate keys, null roots, excess depth,
// trailing data and oversized documents. Resource-specific semantics remain local.
func DecodeSpec(data []byte, dst any) error {
	if len(data) > MaxBytes {
		return fmt.Errorf("configuration exceeds %d bytes", MaxBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("expected object")
	}
	scan := json.NewDecoder(bytes.NewReader(data))
	scan.UseNumber()
	if err := walk(scan, 0); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
func walk(d *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON depth limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("invalid object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if err := walk(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := walk(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected delimiter")
	}
	_, err = d.Token()
	return err
}
