package control

import (
	"encoding/json"
	"testing"

	"github.com/dio/fig/action"
	"github.com/dio/fig/apps/marker"
	"github.com/dio/fig/bundle"
	examples "github.com/dio/fig/examples/config"
	"github.com/dio/fig/match"
)

func TestSetValue(t *testing.T) {
	c := action.Config{Bundle: examples.MarkerInspection, Entry: bundle.Ref{Type: marker.PipelineType, Name: "mark", Version: "1"}, Inputs: map[string]match.Kind{"input.inspection": match.Boolean}}
	for _, bad := range []string{`{}`, `{"match":"select-marker","rule":"absent","value":"ok"}`, `{"match":"absent","rule":"detected","value":"ok"}`, `{"match":"select-marker","rule":"detected","value":null}`, `{"match":"select-marker","rule":"detected","value":"bad space"}`, `{"match":"select-marker","rule":"detected","value":"ok","other":1}`} {
		if _, err := (SetValue{}).Apply(c, json.RawMessage(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	input := json.RawMessage(`{"match":"select-marker","rule":"detected","value":"review-needed"}`)
	changed, err := (SetValue{}).Apply(c, input)
	if err != nil || !changed.Changed {
		t.Fatal(err)
	}
	p, err := marker.CompileWithInputs(changed.Config.Bundle, changed.Config.Entry, c.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	if r := p.Execute(map[string]match.Value{"input.inspection": match.Bool(true)}); r.Value != "review-needed" {
		t.Fatal(r)
	}
	if r := p.Execute(map[string]match.Value{"input.inspection": match.Bool(false)}); r.Value != "waf-clean" {
		t.Fatal(r)
	}
	same, err := (SetValue{}).Apply(changed.Config, input)
	if err != nil || same.Changed {
		t.Fatal(err)
	}
}
