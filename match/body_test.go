package match

import (
	"encoding/json"
	"github.com/dio/fig/body"
	"testing"
)

func TestSharedBodyFacts(t *testing.T) {
	doc, err := body.Parse([]byte(`{"model":"chat","stream":true}`), body.Limits{MaxBytes: 100, MaxDepth: 4, MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{Schema: "fig.match/v1", Name: "body", Revision: "1", Phase: BodyComplete,
		Facts: []FactSpec{
			{Name: "model", Extractor: "body-json-pointer/v1", Type: String, Args: json.RawMessage(`{"pointer":"/model"}`)},
			{Name: "stream", Extractor: "body-json-pointer/v1", Type: Boolean, Args: json.RawMessage(`{"pointer":"/stream"}`)},
		}, Rules: []Rule{{ID: "both", When: Predicate{Op: "all", Children: []Predicate{
			{Op: "equals", Fact: "model", Values: []Value{Text("chat")}},
			{Op: "equals", Fact: "stream", Values: []Value{Bool(true)}},
		}}, Result: json.RawMessage(`"selected"`)}}}
	prepared, err := Prepare[string](spec, Builtins(), func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	// Invalid raw bytes prove both extractors use the shared document, not Body.
	e := prepared.Begin("captured")
	if got := e.Advance(Input{Phase: Headers}); got.Status != Waiting {
		t.Fatal(got)
	}
	got := e.Advance(Input{Phase: BodyComplete, Body: []byte("invalid"), Document: doc})
	if got.Status != Selected || got.Generation != "captured" {
		t.Fatal(got)
	}
	if got := prepared.Begin("1").Advance(Input{Phase: BodyComplete}); got.Status != Failed {
		t.Fatal(got)
	}
	cancelled := prepared.Begin("1")
	cancelled.Cancel()
	if got := cancelled.Advance(Input{Phase: BodyComplete, Document: doc}); got.Status != Cancelled {
		t.Fatal(got)
	}
}
