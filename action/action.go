// Package action describes app-owned, pure configuration operations.
package action

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
)

type Descriptor struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Summary     string          `json:"summary"`
	Effect      string          `json:"effect"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
type Config struct {
	Entry  bundle.Ref            `json:"entry"`
	Bundle json.RawMessage       `json:"bundle"`
	Inputs map[string]match.Kind `json:"inputs,omitempty"`
}
type Change struct {
	Config  Config `json:"config"`
	Changed bool   `json:"changed"`
	Summary string `json:"summary"`
}
type Handler interface {
	Descriptor() Descriptor
	Apply(Config, json.RawMessage) (Change, error)
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Edit changes one resource and re-versions its exact-reference dependency closure.
// Unchanged resources retain their content and versions. Cycles are rejected.
func Edit(c Config, target bundle.Ref, edit func(json.RawMessage) (json.RawMessage, error)) (Change, error) {
	if _, err := bundle.Decode(c.Bundle); err != nil {
		return Change{}, err
	}
	var doc struct {
		APIVersion string            `json:"apiVersion"`
		Scope      string            `json:"scope"`
		Generation string            `json:"generation"`
		Resources  []bundle.Resource `json:"resources"`
	}
	if err := bundle.DecodeSpec(c.Bundle, &doc); err != nil {
		return Change{}, err
	}
	indices := map[bundle.Ref]int{}
	for i, r := range doc.Resources {
		indices[r.Ref] = i
	}
	idx, ok := indices[target]
	if !ok {
		return Change{}, fmt.Errorf("resource not found")
	}
	data, err := edit(bytes.Clone(doc.Resources[idx].Spec))
	if err != nil {
		return Change{}, err
	}
	canonical := func(data []byte) ([]byte, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		return json.Marshal(v)
	}
	before, err := canonical(doc.Resources[idx].Spec)
	if err != nil {
		return Change{}, err
	}
	after, err := canonical(data)
	if err != nil {
		return Change{}, err
	}
	if bytes.Equal(before, after) {
		return Change{Config: c}, nil
	}
	oldSpecs := make([]json.RawMessage, len(doc.Resources))
	for i, r := range doc.Resources {
		oldSpecs[i] = r.Spec
	}
	doc.Resources[idx].Spec = after
	visiting := map[int]bool{}
	done := map[int]bool{}
	var visit func(int) error
	var rewrite func(any) error
	rewrite = func(v any) error {
		switch v := v.(type) {
		case map[string]any:
			if len(v) == 3 {
				typ, _ := v["type"].(string)
				name, _ := v["name"].(string)
				version, _ := v["version"].(string)
				if dep, ok := indices[bundle.Ref{Type: typ, Name: name, Version: version}]; ok {
					if err := visit(dep); err != nil {
						return err
					}
					v["version"] = doc.Resources[dep].Version
					return nil
				}
			}
			for _, item := range v {
				if err := rewrite(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := rewrite(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	visit = func(i int) error {
		if done[i] {
			return nil
		}
		if visiting[i] {
			return fmt.Errorf("cyclic resource references")
		}
		visiting[i] = true
		var v any
		d := json.NewDecoder(bytes.NewReader(doc.Resources[i].Spec))
		d.UseNumber()
		if err := d.Decode(&v); err != nil {
			return err
		}
		if err := rewrite(v); err != nil {
			return err
		}
		next, err := json.Marshal(v)
		if err != nil {
			return err
		}
		original, err := canonical(oldSpecs[i])
		if err != nil {
			return err
		}
		if !bytes.Equal(original, next) {
			doc.Resources[i].Spec = next
			doc.Resources[i].Version = Digest(next)
		}
		done[i] = true
		visiting[i] = false
		return nil
	}
	for i := range doc.Resources {
		if err := visit(i); err != nil {
			return Change{}, err
		}
	}
	entryIndex, ok := indices[c.Entry]
	if !ok {
		return Change{}, fmt.Errorf("entry not found")
	}
	doc.Generation = ""
	encoded, err := json.Marshal(doc)
	if err != nil {
		return Change{}, err
	}
	doc.Generation = Digest(encoded)
	encoded, err = json.Marshal(doc)
	if err != nil {
		return Change{}, err
	}
	c.Bundle = encoded
	c.Entry = doc.Resources[entryIndex].Ref
	return Change{Config: c, Changed: true}, nil
}
