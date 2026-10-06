package control

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dio/fig/action"
	"github.com/dio/fig/apps/waf"
	"github.com/dio/fig/bundle"
	examples "github.com/dio/fig/examples/config"
)

func TestSetMode(t *testing.T) {
	c := action.Config{Bundle: examples.WAF, Entry: bundle.Ref{Type: waf.PipelineType, Name: "edge", Version: "1"}}
	h := SetMode{}
	for _, bad := range []string{`{}`, `null`, `{"policy":null,"mode":"detect"}`, `{"policy":"baseline","mode":"off"}`, `{"policy":"absent","mode":"detect"}`, `{"policy":"baseline","mode":"detect","x":1}`, `{"policy":"baseline","mode":"detect","mode":"enforce"}`} {
		if _, err := h.Apply(c, json.RawMessage(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	change, err := h.Apply(c, json.RawMessage(`{"policy":"baseline","mode":"detect"}`))
	if err != nil || !change.Changed {
		t.Fatal(change, err)
	}
	before, _ := bundle.Decode(c.Bundle)
	after, _ := bundle.Decode(change.Config.Bundle)
	if before.Generation() == after.Generation() || c.Entry == change.Config.Entry {
		t.Fatal("identities unchanged")
	}
	if _, err := waf.Compile(change.Config.Bundle, change.Config.Entry); err != nil {
		t.Fatal(err)
	}
	// A separate policy has unchanged content and identity.
	ref := bundle.Ref{Type: waf.PolicyType, Name: "observe-policy", Version: "1"}
	a, _ := before.Resolve(ref)
	b, err := after.Resolve(ref)
	var av, bv any
	_ = json.Unmarshal(a, &av)
	_ = json.Unmarshal(b, &bv)
	if err != nil || !reflect.DeepEqual(av, bv) {
		t.Fatal("unrelated policy changed", err)
	}
	same, err := h.Apply(change.Config, json.RawMessage(`{"policy":"baseline","mode":"detect"}`))
	if err != nil || same.Changed || string(same.Config.Bundle) != string(change.Config.Bundle) {
		t.Fatal("not a no-op", err)
	}
	original, _ := before.Resolve(bundle.Ref{Type: waf.PolicyType, Name: "baseline", Version: "1"})
	var spec struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(original, &spec)
	if spec.Mode != "enforce" {
		t.Fatal("source mutated")
	}
}
