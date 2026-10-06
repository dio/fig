package bootstrap

import (
	"github.com/dio/fig/handoff"
	"github.com/dio/fig/match"
	"testing"
)

func TestHandoffAdmissionUsesTemplateOrder(t *testing.T) {
	s := handoff.Slot{Activation: "a", Placement: "p", Producer: "w", Name: "s", Type: "t", Scope: "s", Generation: "g", Representation: "r", Phase: match.Headers}
	input := handoff.Input{Slot: s, Field: "matched", Fact: "input.matched", Kind: match.Boolean, Required: true}
	producer := Binding{Name: "P", Data: []byte(`{}`), Export: &s}
	consumer := Binding{Name: "C", Data: []byte(`{}`), Input: &input}
	if _, err := Render([]byte(`["__FIG_P_BUNDLE_CONFIG__","__FIG_C_BUNDLE_CONFIG__"]`), consumer, producer); err != nil {
		t.Fatal(err)
	}
	if _, err := Render([]byte(`["__FIG_C_BUNDLE_CONFIG__","__FIG_P_BUNDLE_CONFIG__"]`), producer, consumer); err == nil {
		t.Fatal("reversed producer accepted")
	}
	if _, err := Render([]byte(`["__FIG_C_BUNDLE_CONFIG__"]`), consumer); err == nil {
		t.Fatal("missing producer accepted")
	}
}
