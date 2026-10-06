package config

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
)

func resource(t *testing.T, mutate func(map[string]any)) bundle.Resource {
	t.Helper()
	s := map[string]any{"phase": "request-headers", "outputType": "test/string",
		"facts": []any{map[string]any{"name": "path", "type": "string", "extractor": "input-field/v1", "args": map[string]any{"name": "path"}}},
		"rules": []any{}, "onNoMatch": map[string]any{"result": "default"},
	}
	if mutate != nil {
		mutate(s)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return bundle.Resource{Ref: bundle.Ref{Type: ResourceType, Name: "headers", Version: "1"}, Spec: raw}
}
func TestHeaderConfig(t *testing.T) {
	for _, name := range []string{"path", "method", "authority"} {
		r := resource(t, func(s map[string]any) { s["facts"].([]any)[0].(map[string]any)["args"] = map[string]any{"name": name} })
		p, err := PrepareHeaders(r, "test/string", func(string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		got := p.Begin("g").Advance(match.Input{Phase: match.Headers})
		if got.Status != match.Selected || !got.Default || got.Value != "default" {
			t.Fatal(got)
		}
	}
	r := resource(t, func(s map[string]any) { s["onNoMatch"] = map[string]any{"return": "no-match"} })
	p, err := PrepareHeaders(r, "test/string", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Begin("g").Advance(match.Input{Phase: match.Headers}); got.Status != match.NoMatch {
		t.Fatal(got)
	}
}
func TestHeaderConfigRejects(t *testing.T) {
	cases := map[string]func(map[string]any){
		"phase":              func(s map[string]any) { s["phase"] = "request-body-complete" },
		"output":             func(s map[string]any) { s["outputType"] = "other" },
		"unknown":            func(s map[string]any) { s["extra"] = true },
		"no-fallback":        func(s map[string]any) { delete(s, "onNoMatch") },
		"ambiguous-fallback": func(s map[string]any) { s["onNoMatch"] = map[string]any{"return": "no-match", "result": "x"} },
		"invalid-return":     func(s map[string]any) { s["onNoMatch"] = map[string]any{"return": "skip"} },
		"field": func(s map[string]any) {
			s["facts"].([]any)[0].(map[string]any)["args"] = map[string]any{"name": "tenant"}
		},
		"args": func(s map[string]any) {
			s["facts"].([]any)[0].(map[string]any)["args"] = map[string]any{"name": "path", "extra": 1}
		},
		"extractor": func(s map[string]any) { s["facts"].([]any)[0].(map[string]any)["extractor"] = "unknown" },
		"type":      func(s map[string]any) { s["facts"].([]any)[0].(map[string]any)["type"] = "integer" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if p, err := PrepareHeaders(resource(t, mutate), "test/string", func(string) error { return nil }); err == nil || p != nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	r := resource(t, nil)
	r.Type = "other"
	if p, err := PrepareHeaders(r, "test/string", func(string) error { return nil }); err == nil || p != nil {
		t.Fatal("wrong resource type")
	}
	if p, err := PrepareHeaders(resource(t, nil), "test/string", func(string) error { return errors.New("invalid output") }); err == nil || p != nil {
		t.Fatal("validator bypassed")
	}
}

func TestPreparedInputs(t *testing.T) {
	r := resource(t, func(s map[string]any) {
		fact := s["facts"].([]any)[0].(map[string]any)
		fact["args"] = map[string]any{"name": "input.matched"}
		fact["type"] = "boolean"
	})
	if _, err := PrepareHeaders(r, "test/string", func(string) error { return nil }); err == nil {
		t.Fatal("undeclared input accepted")
	}
	if _, err := PrepareHeadersWithInputs(r, "test/string", func(string) error { return nil }, map[string]match.Kind{"input.matched": match.Boolean}); err != nil {
		t.Fatal(err)
	}
	for _, fields := range []map[string]match.Kind{
		{"path": match.Boolean}, {"input.matched": match.String}, {"input.matched": "object"},
	} {
		if _, err := PrepareHeadersWithInputs(r, "test/string", func(string) error { return nil }, fields); err == nil {
			t.Fatal("invalid fields accepted")
		}
	}
}
