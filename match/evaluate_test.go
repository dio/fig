package match

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestEvaluationOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fields   map[string]Value
		fallback json.RawMessage
		status   Status
		code     string
	}{
		{"selected", map[string]Value{"x": Text("yes")}, nil, Selected, ""},
		{"no-match", map[string]Value{"x": Text("no")}, nil, NoMatch, ""},
		{"missing", nil, nil, NoMatch, ""},
		{"default", nil, json.RawMessage(`"fallback"`), Selected, ""},
		{"wrong-type", map[string]Value{"x": Int(1)}, json.RawMessage(`"fallback"`), Failed, CodeInvalidType},
		{"ambiguous-value", map[string]Value{"x": {Kind: String, Text: "yes", Bool: true}}, nil, Failed, CodeInvalidType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			s.Default = tc.fallback
			// Overlapping rules must retain first-match ordering.
			second := s.Rules[0]
			second.ID = "second"
			second.Result = json.RawMessage(`"two"`)
			s.Rules = append(s.Rules, second)
			e := mustPrepare(t, s).Begin("g1")
			got := e.Advance(Input{Phase: Headers, Fields: tc.fields})
			if got.Status != tc.status || got.Code != tc.code {
				t.Fatal(got)
			}
			if got.View != "test" || got.Revision != "r1" || got.Generation != "g1" {
				t.Fatal(got)
			}
			if tc.name == "selected" && (got.RuleID != "first" || got.Value != "one" || got.Default) {
				t.Fatal(got)
			}
			if tc.name == "default" && (!got.Default || got.RuleID != "" || got.Value != "fallback") {
				t.Fatal(got)
			}
			if again := e.Advance(Input{Phase: "bad"}); again != got {
				t.Fatalf("terminal changed: %+v %+v", got, again)
			}
			if again := e.Cancel(); again != got {
				t.Fatal("cancel changed terminal")
			}
		})
	}
}
func TestWaitingAndCancellation(t *testing.T) {
	s := validSpec()
	s.Phase = BodyComplete
	p := mustPrepare(t, s)
	e := p.Begin("g1")
	for range 2 {
		if got := e.Advance(Input{Phase: Headers}); got.Status != Waiting {
			t.Fatal(got)
		}
	}
	got := e.Advance(Input{Phase: BodyComplete, Fields: map[string]Value{"x": Text("yes")}})
	if got.Status != Selected {
		t.Fatal(got)
	}
	c := p.Begin("g1")
	if got := c.Cancel(); got.Status != Cancelled {
		t.Fatal(got)
	}
	if got := c.Advance(Input{Phase: BodyComplete}); got.Status != Cancelled {
		t.Fatal(got)
	}
	if got := p.Begin("g").Advance(Input{Phase: "unknown"}); got.Code != CodeInvalidPhase {
		t.Fatal(got)
	}
	// Earlier input is not implicitly accumulated into a later snapshot.
	e = p.Begin("g1")
	e.Advance(Input{Phase: Headers, Fields: map[string]Value{"x": Text("yes")}})
	if got := e.Advance(Input{Phase: BodyComplete}); got.Status != NoMatch {
		t.Fatal(got)
	}
}
func TestExtractorFailureNeverDefaults(t *testing.T) {
	for _, tc := range []struct {
		state FactState
		code  string
	}{
		{Invalid, CodeInvalidFact}, {Pending, CodeExtractorContract}, {"unknown", CodeExtractorContract},
	} {
		s := validSpec()
		s.Default = json.RawMessage(`"fallback"`)
		registry := Registry{InputField: func(FactSpec) (Phase, Extractor, error) {
			return Headers, func(Input) Fact { return Fact{State: tc.state, Code: "private user input"} }, nil
		}}
		p, err := Prepare(s, registry, accept)
		if err != nil {
			t.Fatal(err)
		}
		got := p.Begin("g").Advance(Input{Phase: Headers})
		if got.Status != Failed || got.Code != tc.code || got.Default || got.Value != "" {
			t.Fatal(got)
		}
	}
}
func TestConcurrentAdvanceAndCancel(t *testing.T) {
	p := mustPrepare(t, validSpec())
	for range 50 {
		e := p.Begin("g")
		results := make(chan Result[string], 20)
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i%2 == 0 {
					results <- e.Cancel()
					return
				}
				results <- e.Advance(Input{Phase: Headers, Fields: map[string]Value{"x": Text("yes")}})
			}()
		}
		wg.Wait()
		close(results)
		final := e.Cancel()
		if final.Status != Selected && final.Status != Cancelled {
			t.Fatal(final)
		}
		for got := range results {
			if got != final {
				t.Fatalf("inconsistent terminal: %+v %+v", got, final)
			}
		}
	}
}
func TestValueValidation(t *testing.T) {
	for _, v := range []Value{Text(""), Bool(false), Bool(true), Int(0), Int(-1)} {
		if !v.valid() {
			t.Fatal(v)
		}
	}
	for _, v := range []Value{{}, {Kind: "unknown"}, {Kind: String, Int: 1}, {Kind: Boolean, Text: "x"}, {Kind: Integer, Bool: true}} {
		if v.valid() {
			t.Fatal(v)
		}
	}
}
