package action

import (
	"encoding/json"
	"testing"

	"github.com/dio/fig/bundle"
)

func TestEdit(t *testing.T) {
	c := Config{Entry: bundle.Ref{Type: "pipe", Name: "p", Version: "1"}, Bundle: json.RawMessage(`{"apiVersion":"fig/v1alpha1","scope":"demo","generation":"1","resources":[{"type":"value","name":"v","version":"1","spec":{"number":9007199254740993}},{"type":"pipe","name":"p","version":"1","spec":{"ref":{"type":"value","name":"v","version":"1"}}}]}`)}
	ref := bundle.Ref{Type: "value", Name: "v", Version: "1"}
	noOp, err := Edit(c, ref, func(data json.RawMessage) (json.RawMessage, error) { return data, nil })
	if err != nil || noOp.Changed {
		t.Fatal(err)
	}
	changed, err := Edit(c, ref, func(data json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"number":9007199254740995}`), nil
	})
	if err != nil || !changed.Changed {
		t.Fatal(err)
	}
	if changed.Config.Entry.Version == "1" {
		t.Fatal("entry not rebound")
	}
	b, err := bundle.Decode(changed.Config.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.Resolve(changed.Config.Entry)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Ref bundle.Ref `json:"ref"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	raw, err = b.Resolve(spec.Ref)
	if err != nil || string(raw) != `{"number":9007199254740995}` {
		t.Fatal(string(raw), err)
	}
}
