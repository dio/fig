// Package handoff adapts Fig's scalar handoff contract to Envoy filter state.
package handoff

import (
	"bytes"

	core "github.com/dio/fig/handoff"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// State is the small SDK surface needed by this carrier. Calls must be made on
// the stream's callback thread. This does not provide atomic concurrent writes.
type State interface {
	GetFilterState(string) (shared.UnsafeEnvoyBuffer, bool)
	SetFilterState(string, []byte)
}

func Read(h State, s core.Slot) (core.Record, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	raw, ok := h.GetFilterState(s.Key())
	if !ok {
		return nil, core.ErrMissing
	}
	if raw.Len > core.MaxBytes {
		return nil, core.ErrLimit
	}
	return core.Decode(s, bytes.Clone(raw.ToUnsafeBytes()))
}
func Publish(h State, s core.Slot, r core.Record) error {
	data, err := core.Encode(s, r)
	if err != nil {
		return err
	}
	if _, ok := h.GetFilterState(s.Key()); ok {
		return core.ErrConflict
	}
	h.SetFilterState(s.Key(), data)
	raw, ok := h.GetFilterState(s.Key())
	if !ok {
		return core.ErrStore
	}
	if raw.Len > core.MaxBytes || !bytes.Equal(raw.ToUnsafeBytes(), data) {
		return core.ErrStore
	}
	return nil
}
