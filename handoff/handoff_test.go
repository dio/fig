package handoff

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/dio/fig/match"
)

func testSlot() Slot {
	return Slot{Activation: "a1", Placement: "edge", Producer: "waf-a", Name: "inspection", Type: "test/v1", Scope: "demo", Generation: "1", Representation: "headers", Phase: match.Headers}
}
func TestEnvelope(t *testing.T) {
	s := testSlot()
	record := Record{"matched": match.Bool(true), "policy": match.Text("original")}
	data, err := Encode(s, record)
	if err != nil {
		t.Fatal(err)
	}
	record["policy"] = match.Text("changed")
	got, err := Decode(s, data)
	if err != nil || got["policy"].Text != "original" {
		t.Fatal(got, err)
	}
	got["policy"] = match.Text("mutated")
	again, _ := Decode(s, data)
	if again["policy"].Text != "original" {
		t.Fatal("shared decoded map")
	}
	for _, change := range []func(*Slot){
		func(s *Slot) { s.Activation = "a2" }, func(s *Slot) { s.Placement = "other" }, func(s *Slot) { s.Producer = "other" },
		func(s *Slot) { s.Name = "other" }, func(s *Slot) { s.Type = "other" }, func(s *Slot) { s.Scope = "other" },
		func(s *Slot) { s.Generation = "2" }, func(s *Slot) { s.Representation = "other" },
	} {
		want := s
		change(&want)
		if _, err := Decode(want, data); !errors.Is(err, ErrMismatch) {
			t.Fatal(err)
		}
	}
	for _, bad := range [][]byte{nil, []byte(`{}`), append(bytes.Clone(data), []byte(` {}`)...), bytes.Replace(data, []byte(`"version":`), []byte(`"version":"duplicate","version":`), 1)} {
		if _, err := Decode(s, bad); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	if _, err := Decode(s, bytes.Repeat([]byte("x"), MaxBytes+1)); err != ErrLimit {
		t.Fatal(err)
	}
	for _, bad := range []Record{nil, {"x": {Kind: match.Boolean, Text: "ambiguous"}}, {"x": match.Text(strings.Repeat("x", 1025))}, {"x": match.Text(string([]byte{255}))}} {
		if _, err := Encode(s, bad); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
	large := Record{}
	for _, k := range []string{"a", "b", "c", "d"} {
		large[k] = match.Text(strings.Repeat("x", 1024))
	}
	if _, err := Encode(s, large); err != ErrLimit {
		t.Fatal(err)
	}
}
func TestBindings(t *testing.T) {
	s := testSlot()
	i := Input{Slot: s, Field: "matched", Fact: "input.inspection", Kind: match.Boolean, Required: true}
	if err := ValidateChain([]Stage{{Export: &s}, {Input: &i}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateChain([]Stage{{Input: &i}, {Export: &s}}); err != ErrMissing {
		t.Fatal(err)
	}
	if err := ValidateChain([]Stage{{Export: &s}, {Export: &s}}); err != ErrConflict {
		t.Fatal(err)
	}
	i.Slot.Generation = "2"
	if err := ValidateChain([]Stage{{Export: &s}, {Input: &i}}); err != ErrMismatch {
		t.Fatal(err)
	}
	i.Slot = s
	i.Required = false
	if err := ValidateChain([]Stage{{Input: &i}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateChain([]Stage{{Input: &i}, {Export: &s}}); err != ErrMissing {
		t.Fatal("optional forward dependency accepted", err)
	}
	i.Fact = "path"
	if i.Validate() == nil {
		t.Fatal("native field shadowed")
	}
	i.Fact = "input.inspection"
	if _, err := i.Project(Record{"matched": match.Text("true")}); err == nil {
		t.Fatal("wrong type")
	}
	if _, err := i.Project(Record{}); err == nil {
		t.Fatal("missing field")
	}
	if err := ValidateChain(make([]Stage, MaxSlots+1)); err != ErrLimit {
		t.Fatal(err)
	}
	s.Phase = match.BodyComplete
	if s.Validate() == nil {
		t.Fatal("body handoff accepted")
	}
}
func FuzzDecode(f *testing.F) {
	seed, _ := Encode(testSlot(), Record{"matched": match.Bool(true)})
	f.Add(seed)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxBytes+1 {
			return
		}
		r, err := Decode(testSlot(), data)
		if err != nil {
			if r != nil {
				t.Fatal("partial result")
			}
			return
		}
		encoded, err := Encode(testSlot(), r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(testSlot(), encoded); err != nil {
			t.Fatal(err)
		}
	})
}
