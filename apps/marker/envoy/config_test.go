package envoy

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dio/fig/apps/marker"
	"github.com/dio/fig/bundle"
	"github.com/dio/fig/handoff"
	"github.com/dio/fig/match"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

func TestInputContract(t *testing.T) {
	data, err := os.ReadFile("../../../examples/config/marker-inspection.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valid", "missing-binding", "kind", "shadow", "phase"} {
		t.Run(name, func(t *testing.T) {
			input := &handoff.Input{Slot: handoff.Slot{Activation: "local", Placement: "edge", Producer: "waf-a", Name: "inspection", Type: "example/v1", Scope: "demo", Generation: "1", Representation: "headers", Phase: match.Headers}, Field: "matched", Fact: "input.inspection", Kind: match.Boolean, Required: true}
			switch name {
			case "missing-binding":
				input = nil
			case "kind":
				input.Kind = match.String
			case "shadow":
				input.Fact = "path"
			case "phase":
				input.Slot.Phase = match.BodyComplete
			}
			cfg := appConfig{Entry: bundle.Ref{Type: marker.PipelineType, Name: "mark", Version: "1"}, Bundle: data, Input: input}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			factory, err := (&ConfigFactory{}).Create(nil, raw)
			if name == "valid" {
				if err != nil || factory == nil {
					t.Fatal(err)
				}
			} else if err == nil || factory != nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
}
func TestCompletedStreamCannotRead(t *testing.T) {
	filter := &markerAppFilter{}
	filter.OnStreamComplete()
	if filter.OnRequestHeaders(nil, true) != shared.HeadersStatusStop {
		t.Fatal("completed stream continued")
	}
}
