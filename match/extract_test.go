package match

import (
	"encoding/json"
	"testing"
)

func TestBuiltinRegistryIsolation(t *testing.T) {
	r := Builtins()
	delete(r, InputField)
	if Builtins()[InputField] == nil {
		t.Fatal("shared mutable registry")
	}
}
func TestExtractorArguments(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{InputField, `{`}, {InputField, `{"name":""}`}, {InputField, `{"name":"x"} {}`},
		{InputField, `{"name":"x","extra":1}`},
		{JSONPointer, `{"pointer":"/x","maxBytes":0,"maxDepth":1}`},
		{JSONPointer, `{"pointer":"/x","maxBytes":1,"maxDepth":129}`},
		{JSONPointer, `{"pointer":"bad","maxBytes":10,"maxDepth":2}`},
		{JSONPointer, `{"pointer":"/x","maxBytes":"bad"}`},
		{BodyJSONPointer, `{"pointer":"/~2"}`}, {BodyJSONPointer, `{"extra":true}`},
	} {
		if _, fn, err := Builtins()[tc.name](FactSpec{Args: json.RawMessage(tc.args)}); err == nil || fn != nil {
			t.Errorf("accepted %s %s", tc.name, tc.args)
		}
	}
}
func TestLegacyJSONFacts(t *testing.T) {
	for _, tc := range []struct {
		name, raw, pointer string
		state              FactState
		value              Value
		code               string
	}{
		{"string", `{"x":"yes"}`, "/x", Present, Text("yes"), ""},
		{"boolean", `{"x":false}`, "/x", Present, Bool(false), ""},
		{"integer", `{"x":-1}`, "/x", Present, Int(-1), ""},
		{"missing", `{}`, "/x", Missing, Value{}, ""},
		{"null", `{"x":null}`, "/x", Invalid, Value{}, CodeInvalidType},
		{"array", `{"x":[]}`, "/x", Invalid, Value{}, CodeInvalidType},
		{"fraction", `{"x":1.0}`, "/x", Invalid, Value{}, CodeInvalidType},
		{"malformed", `{`, "/x", Invalid, Value{}, CodeInvalidJSON},
		{"duplicate", `{"x":1,"x":2}`, "/x", Invalid, Value{}, CodeInvalidJSON},
		{"depth", `[[[]]]`, "", Invalid, Value{}, CodeInvalidJSON},
		{"escaped-pointer", `{"a/b":{"~":true}}`, "/a~1b/~0", Present, Bool(true), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"pointer": tc.pointer, "maxBytes": 100, "maxDepth": 2})
			phase, fn, err := Builtins()[JSONPointer](FactSpec{Args: args})
			if err != nil {
				t.Fatal(err)
			}
			if phase != BodyComplete {
				t.Fatal(phase)
			}
			got := fn(Input{Body: []byte(tc.raw)})
			if got.State != tc.state || got.Value != tc.value || got.Code != tc.code {
				t.Fatal(got)
			}
		})
	}
	_, fn, err := Builtins()[JSONPointer](FactSpec{Args: json.RawMessage(`{"pointer":"","maxBytes":2,"maxDepth":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got := fn(Input{Body: []byte(`"ok"`)}); got.Code != CodeBodyLimit {
		t.Fatal(got)
	}
}
func FuzzPrepareAndEvaluate(f *testing.F) {
	data, _ := json.Marshal(validSpec())
	f.Add(data, "yes")
	f.Add([]byte(`{}`), "no")
	f.Fuzz(func(t *testing.T, data []byte, input string) {
		if len(data) > 8192 || len(input) > 1024 {
			return
		}
		var s Spec
		if err := json.Unmarshal(data, &s); err != nil {
			return
		}
		p, err := Prepare(s, Builtins(), accept)
		if err != nil {
			if p != nil {
				t.Fatal("partial prepared view")
			}
			return
		}
		e := p.Begin("fuzz")
		e.Advance(Input{Phase: Headers, Fields: map[string]Value{"x": Text(input)}})
		result := e.Advance(Input{Phase: BodyComplete, Body: []byte(input)})
		if result.Status == Waiting {
			t.Fatal("waiting at final phase")
		}
		if e.Cancel() != result {
			t.Fatal("terminal changed")
		}
	})
}
