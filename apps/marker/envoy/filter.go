package envoy

import (
	"errors"
	"strings"

	"github.com/dio/fig/apps/marker"
	"github.com/dio/fig/handoff"
	carrier "github.com/dio/fig/hosts/envoy/handoff"
	"github.com/dio/fig/match"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// ConfigFactory compiles app configuration for an Envoy filter instance.
type ConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}
type markerAppFactory struct {
	shared.EmptyHttpFilterFactory
	prepared *marker.Prepared
	input    *handoff.Input
}

func (*ConfigFactory) Create(_ shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	config, err := decodeAppConfig(data)
	if err != nil {
		return nil, err
	}
	inputs := map[string]match.Kind{}
	if config.Input != nil {
		if err := config.Input.Validate(); err != nil {
			return nil, err
		}
		inputs[config.Input.Fact] = config.Input.Kind
	}
	prepared, err := marker.CompileWithInputs(config.Bundle, config.Entry, inputs)
	if err != nil {
		return nil, err
	}
	return &markerAppFactory{prepared: prepared, input: config.Input}, nil
}
func (f *markerAppFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &markerAppFilter{prepared: f.prepared, handle: h, input: f.input}
}

type markerAppFilter struct {
	shared.EmptyHttpFilter
	prepared  *marker.Prepared
	handle    shared.HttpFilterHandle
	result    marker.Result
	evaluated bool
	ended     bool
	input     *handoff.Input
}

func (f *markerAppFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if f.ended {
		return shared.HeadersStatusStop
	}
	headers.Remove("x-fig-marker")
	fields := map[string]match.Value{
		"path":      match.Text(strings.SplitN(headers.GetOne(":path").ToString(), "?", 2)[0]),
		"method":    match.Text(headers.GetOne(":method").ToString()),
		"authority": match.Text(headers.GetOne(":authority").ToString()),
	}
	if f.input != nil {
		values, err := carrier.Read(f.handle, f.input.Slot)
		optionalMissing := errors.Is(err, handoff.ErrMissing) && !f.input.Required
		if err != nil && !optionalMissing {
			return f.handoffFailure()
		}
		if err == nil {
			value, err := f.input.Project(values)
			if err != nil {
				return f.handoffFailure()
			}
			fields[f.input.Fact] = value
		}
	}
	f.result = f.prepared.Execute(fields)
	f.evaluated = true
	r := f.result
	f.handle.SetMetadata("fig.marker", "outcome", map[string]any{"value": r.Value, "action": r.Action, "scope": r.Scope, "generation": r.Generation})
	if r.Status != 0 {
		f.handle.SendLocalResponse(uint32(r.Status), nil, []byte(`{"error":"marker_failed"}`), "fig_marker_failed")
		return shared.HeadersStatusStop
	}
	if r.Value != "" {
		headers.Set("x-fig-marker", r.Value)
	}
	return shared.HeadersStatusContinue
}
func (f *markerAppFilter) OnResponseHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if f.evaluated {
		headers.Remove("x-fig-marker")
		if f.result.Value != "" {
			headers.Set("x-fig-marker", f.result.Value)
		}
		headers.Set("x-fig-marker-action", f.result.Action)
	}
	return shared.HeadersStatusContinue
}

func (f *markerAppFilter) handoffFailure() shared.HeadersStatus {
	f.handle.SendLocalResponse(500, nil, []byte(`{"error":"handoff_failed"}`), "fig_handoff_failed")
	return shared.HeadersStatusStop
}
func (f *markerAppFilter) OnStreamComplete() { f.ended = true }
