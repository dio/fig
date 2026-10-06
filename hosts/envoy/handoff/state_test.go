package handoff

import (
	"bytes"
	"testing"

	core "github.com/dio/fig/handoff"
	"github.com/dio/fig/match"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

type state struct {
	values        map[string][]byte
	fail, corrupt bool
}

func (s *state) GetFilterState(key string) (shared.UnsafeEnvoyBuffer, bool) {
	value := s.values[key]
	if len(value) == 0 {
		return shared.UnsafeEnvoyBuffer{}, false
	}
	return shared.UnsafeEnvoyBuffer{Ptr: &value[0], Len: uint64(len(value))}, true
}
func (s *state) SetFilterState(key string, value []byte) {
	if s.fail {
		return
	}
	s.values[key] = bytes.Clone(value)
	if s.corrupt {
		s.values[key][0] = '!'
	}
}
func slot() core.Slot {
	return core.Slot{Activation: "a", Placement: "p", Producer: "w", Name: "s", Type: "test/v1", Scope: "demo", Generation: "1", Representation: "h", Phase: match.Headers}
}
func TestCarrier(t *testing.T) {
	h := &state{values: map[string][]byte{}}
	s := slot()
	record := core.Record{"matched": match.Bool(true)}
	if _, err := Read(h, s); err != core.ErrMissing {
		t.Fatal(err)
	}
	if err := Publish(h, s, record); err != nil {
		t.Fatal(err)
	}
	if err := Publish(h, s, record); err != core.ErrConflict {
		t.Fatal(err)
	}
	r, err := Read(h, s)
	if err != nil || !r["matched"].Bool {
		t.Fatal(r, err)
	}
	h.values[s.Key()][0] = '!'
	if !r["matched"].Bool {
		t.Fatal("borrowed state retained")
	}
	if _, err := Read(h, s); err != core.ErrInvalid {
		t.Fatal(err)
	}
	h.values[s.Key()] = bytes.Repeat([]byte("x"), core.MaxBytes+1)
	if _, err := Read(h, s); err != core.ErrLimit {
		t.Fatal(err)
	}
	for _, failure := range []state{{values: map[string][]byte{}, fail: true}, {values: map[string][]byte{}, corrupt: true}} {
		if err := Publish(&failure, s, record); err != core.ErrStore {
			t.Fatal(err)
		}
	}
	// Independent streams never share their state maps.
	other := &state{values: map[string][]byte{}}
	if _, err := Read(other, s); err != core.ErrMissing {
		t.Fatal(err)
	}
}
