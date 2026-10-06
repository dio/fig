package envoy

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dio/fig/apps/waf"
	"github.com/dio/fig/bundle"
	"github.com/dio/fig/handoff"
	"github.com/dio/fig/match"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

func TestExportContract(t *testing.T) {
	data, err := os.ReadFile("../../../examples/config/waf.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valid", "scope", "generation", "type", "slot", "representation", "phase"} {
		t.Run(name, func(t *testing.T) {
			slot := handoff.Slot{Activation: "local", Placement: "edge", Producer: "waf-a", Name: "inspection", Type: waf.OutcomeType, Scope: "demo", Generation: "1", Representation: "request-headers@waf-entry", Phase: match.Headers}
			switch name {
			case "scope":
				slot.Scope = "wrong"
			case "generation":
				slot.Generation = "wrong"
			case "type":
				slot.Type = "wrong"
			case "slot":
				slot.Name = "wrong"
			case "representation":
				slot.Representation = "wrong"
			case "phase":
				slot.Phase = match.BodyComplete
			}
			cfg := appConfig{Entry: bundle.Ref{Type: waf.PipelineType, Name: "edge", Version: "1"}, Bundle: data, Export: &slot}
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
				t.Fatal("invalid export accepted")
			}
		})
	}
}
func TestCompletedStreamCannotPublish(t *testing.T) {
	filter := &wafAppFilter{}
	filter.OnStreamComplete()
	if filter.OnRequestHeaders(nil, true) != shared.HeadersStatusStop {
		t.Fatal("completed stream continued")
	}
}
