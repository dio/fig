package match

import (
	"encoding/json"
	"errors"
	"testing"
)

func validSpec() Spec {
	return Spec{Schema: Schema, Name: "test", Revision: "r1", Phase: Headers,
		Facts: []FactSpec{{Name: "x", Extractor: InputField, Type: String, Args: json.RawMessage(`{"name":"x"}`)}},
		Rules: []Rule{{ID: "first", When: Predicate{Op: OpEquals, Fact: "x", Values: []Value{Text("yes")}}, Result: json.RawMessage(`"one"`)}},
	}
}
func accept(string) error { return nil }
func mustPrepare(t *testing.T, s Spec) *Prepared[string] {
	t.Helper()
	p, err := Prepare(s, Builtins(), accept)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPrepareRejections(t *testing.T) {
	cases := map[string]func(*Spec){
		"schema":         func(s *Spec) { s.Schema = "wrong" },
		"name":           func(s *Spec) { s.Name = "" },
		"revision":       func(s *Spec) { s.Revision = "" },
		"phase":          func(s *Spec) { s.Phase = "future" },
		"empty-fact":     func(s *Spec) { s.Facts[0].Name = "" },
		"duplicate-fact": func(s *Spec) { s.Facts = append(s.Facts, s.Facts[0]) },
		"kind":           func(s *Spec) { s.Facts[0].Type = "float" },
		"extractor":      func(s *Spec) { s.Facts[0].Extractor = "missing" },
		"args":           func(s *Spec) { s.Facts[0].Args = json.RawMessage(`{"name":"x","extra":1}`) },
		"late-fact": func(s *Spec) {
			s.Facts[0].Extractor = BodyJSONPointer
			s.Facts[0].Args = json.RawMessage(`{"pointer":"/x"}`)
		},
		"empty-rule":      func(s *Spec) { s.Rules[0].ID = "" },
		"duplicate-rule":  func(s *Spec) { s.Rules = append(s.Rules, s.Rules[0]) },
		"predicate":       func(s *Spec) { s.Rules[0].When.Op = "bad" },
		"output-null":     func(s *Spec) { s.Rules[0].Result = json.RawMessage(`null`) },
		"output-type":     func(s *Spec) { s.Rules[0].Result = json.RawMessage(`{}`) },
		"output-trailing": func(s *Spec) { s.Rules[0].Result = json.RawMessage(`"ok" "extra"`) },
		"default":         func(s *Spec) { s.Default = json.RawMessage(`null`) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := validSpec()
			mutate(&s)
			if p, err := Prepare(s, Builtins(), accept); err == nil || p != nil {
				t.Fatalf("accepted invalid spec: %v", err)
			}
		})
	}
	if p, err := Prepare[string](validSpec(), Builtins(), nil); err == nil || p != nil {
		t.Fatal("nil validator accepted")
	}
	for _, which := range []string{"rule", "default"} {
		t.Run("validate-"+which, func(t *testing.T) {
			s := validSpec()
			if which == "default" {
				s.Rules = nil
				s.Default = json.RawMessage(`"one"`)
			}
			if p, err := Prepare(s, Builtins(), func(string) error { return errors.New("unresolved") }); err == nil || p != nil {
				t.Fatal("validation bypassed")
			}
		})
	}
}
func TestFactoryContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   Phase
		extract Extractor
		err     error
	}{
		{name: "error", err: errors.New("failed")},
		{name: "nil", phase: Headers},
		{name: "phase", phase: "bad", extract: func(Input) Fact { return Fact{State: Missing} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := Registry{InputField: func(FactSpec) (Phase, Extractor, error) { return tc.phase, tc.extract, tc.err }}
			if p, err := Prepare(validSpec(), registry, accept); err == nil || p != nil {
				t.Fatal("bad factory accepted")
			}
		})
	}
	// Factory gets an owned copy of Args, even when it retains the bytes.
	s := validSpec()
	var retained []byte
	registry := Registry{InputField: func(f FactSpec) (Phase, Extractor, error) {
		retained = f.Args
		return Headers, func(Input) Fact { return Fact{State: Missing} }, nil
	}}
	if _, err := Prepare(s, registry, accept); err != nil {
		t.Fatal(err)
	}
	s.Facts[0].Args[0] = 'x'
	if retained[0] != '{' {
		t.Fatal("caller bytes retained")
	}
}
func TestPrepareOwnsSpecAndResults(t *testing.T) {
	s := validSpec()
	s.Rules[0].Result = json.RawMessage(`{"items":["original"]}`)
	type output struct {
		Items []string `json:"items"`
	}
	p, err := Prepare(s, Builtins(), func(output) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	s.Rules[0].When.Values[0] = Text("changed")
	s.Rules[0].Result[0] = '!'
	s.Facts[0].Args[0] = '!'
	e := p.Begin("captured")
	input := Input{Phase: Headers, Fields: map[string]Value{"x": Text("yes")}}
	got := e.Advance(input)
	if got.Status != Selected || got.Value.Items[0] != "original" {
		t.Fatal(got)
	}
	got.Value.Items[0] = "mutated"
	again := e.Cancel()
	if again.Value.Items[0] != "original" || again.Generation != "captured" {
		t.Fatal(again)
	}
	if got := p.Begin("next").Advance(input); got.Value.Items[0] != "original" {
		t.Fatal(got)
	}
}
func TestPredicateTruthTable(t *testing.T) {
	leaf := func(op string, values ...Value) Predicate { return Predicate{Op: op, Fact: "x", Values: values} }
	yes := leaf(OpEquals, Text("yes"))
	no := leaf(OpEquals, Text("no"))
	for _, tc := range []struct {
		name             string
		p                Predicate
		yes, no, missing bool
	}{
		{"equals", yes, true, false, false},
		{"in", leaf(OpIn, Text("yes"), Text("no")), true, true, false},
		{"exists", leaf(OpExists), true, true, false},
		{"missing", leaf(OpIsMissing), false, false, true},
		{"not", Predicate{Op: OpNot, Children: []Predicate{yes}}, false, true, true},
		{"all", Predicate{Op: OpAll, Children: []Predicate{yes, no}}, false, false, false},
		{"all-true", Predicate{Op: OpAll, Children: []Predicate{yes, yes}}, true, false, false},
		{"any", Predicate{Op: OpAny, Children: []Predicate{yes, no}}, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn, err := compilePredicate(tc.p, map[string]Kind{"x": String}, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, in := range []struct {
				fact Fact
				want bool
			}{
				{Fact{State: Present, Value: Text("yes")}, tc.yes},
				{Fact{State: Present, Value: Text("no")}, tc.no},
				{Fact{State: Missing}, tc.missing},
			} {
				if got := fn(map[string]Fact{"x": in.fact}); got != in.want {
					t.Fatalf("%+v got %v", in.fact, got)
				}
			}
		})
	}
}
func TestPredicateRejectionsAndDepth(t *testing.T) {
	leaf := Predicate{Op: OpExists, Fact: "x"}
	cases := []Predicate{
		{Op: "bad"}, {Op: OpAll}, {Op: OpAny, Fact: "x", Children: []Predicate{leaf}},
		{Op: OpAll, Values: []Value{Text("x")}, Children: []Predicate{leaf}},
		{Op: OpNot, Children: []Predicate{leaf, leaf}},
		{Op: OpAll, Children: []Predicate{{Op: "bad"}}},
		{Op: OpExists, Fact: "missing"}, {Op: OpExists, Fact: "x", Children: []Predicate{leaf}},
		{Op: OpEquals, Fact: "x"}, {Op: OpIn, Fact: "x"},
		{Op: OpExists, Fact: "x", Values: []Value{Text("x")}},
		{Op: OpEquals, Fact: "x", Values: []Value{Bool(true)}},
		{Op: OpEquals, Fact: "x", Values: []Value{{Kind: String, Int: 1}}},
	}
	for i, p := range cases {
		if _, err := compilePredicate(p, map[string]Kind{"x": String}, 0); err == nil {
			t.Errorf("accepted case %d", i)
		}
	}
	p := leaf
	for range maxPredicateDepth {
		p = Predicate{Op: OpNot, Children: []Predicate{p}}
	}
	if _, err := compilePredicate(p, map[string]Kind{"x": String}, 0); err != nil {
		t.Fatal("boundary rejected", err)
	}
	p = Predicate{Op: OpNot, Children: []Predicate{p}}
	if _, err := compilePredicate(p, map[string]Kind{"x": String}, 0); err == nil {
		t.Fatal("depth exceeded")
	}
}
