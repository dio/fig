package envoy

import (
	"strings"

	"github.com/dio/fig/apps/marker"
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
}

func (*ConfigFactory) Create(_ shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	config, err := decodeAppConfig(data)
	if err != nil {
		return nil, err
	}
	prepared, err := marker.Compile(config.Bundle, config.Entry)
	if err != nil {
		return nil, err
	}
	return &markerAppFactory{prepared: prepared}, nil
}
func (f *markerAppFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &markerAppFilter{prepared: f.prepared, handle: h}
}

type markerAppFilter struct {
	shared.EmptyHttpFilter
	prepared  *marker.Prepared
	handle    shared.HttpFilterHandle
	result    marker.Result
	evaluated bool
}

func (f *markerAppFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	headers.Remove("x-fig-marker")
	f.result = f.prepared.Execute(map[string]match.Value{
		"path":      match.Text(strings.SplitN(headers.GetOne(":path").ToString(), "?", 2)[0]),
		"method":    match.Text(headers.GetOne(":method").ToString()),
		"authority": match.Text(headers.GetOne(":authority").ToString()),
	})
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
