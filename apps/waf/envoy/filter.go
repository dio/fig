package envoy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	wafapp "github.com/dio/fig/apps/waf"
	"github.com/dio/fig/apps/waf/inspect"
	"github.com/dio/fig/bundle"
	"github.com/dio/fig/handoff"
	carrier "github.com/dio/fig/hosts/envoy/handoff"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// ConfigFactory compiles app configuration for an Envoy filter instance.
type ConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}
type wafAppFactory struct {
	shared.EmptyHttpFilterFactory
	prepared *wafapp.Prepared
	export   *handoff.Slot
}

func (*ConfigFactory) Create(_ shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	bootstrap, err := decodeAppConfig(data)
	if err != nil {
		return nil, err
	}
	prepared, err := wafapp.Compile(bootstrap.Bundle, bootstrap.Entry)
	if err != nil {
		return nil, err
	}
	if bootstrap.Export != nil {
		slot := *bootstrap.Export
		if err := slot.Validate(); err != nil {
			return nil, err
		}
		b, err := bundle.Decode(bootstrap.Bundle)
		if err != nil {
			return nil, err
		}
		if slot.Type != wafapp.OutcomeType || slot.Name != "inspection" || slot.Scope != b.Scope() || slot.Generation != b.Generation() || slot.Representation != "request-headers@waf-entry" {
			return nil, fmt.Errorf("WAF export does not match installed contract or bundle identity")
		}
	}
	return &wafAppFactory{prepared: prepared, export: bootstrap.Export}, nil
}
func (f *wafAppFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &wafAppFilter{prepared: f.prepared, handle: h, export: f.export}
}

type wafAppFilter struct {
	shared.EmptyHttpFilter
	prepared  *wafapp.Prepared
	handle    shared.HttpFilterHandle
	result    wafapp.Result
	evaluated bool
	ended     bool
	export    *handoff.Slot
}

func (f *wafAppFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if f.ended {
		return shared.HeadersStatusStop
	}
	for _, name := range []string{"x-fig-policy", "x-fig-waf-policy", "x-fig-waf-action", "x-fig-waf-matched", "x-fig-waf-rule"} {
		headers.Remove(name)
	}
	request := inspect.Request{Method: headers.GetOne(":method").ToString(), URI: headers.GetOne(":path").ToString(),
		Authority: headers.GetOne(":authority").ToString(), Protocol: "HTTP/1.1", Headers: [][2]string{}}
	if protocol, ok := f.handle.GetAttributeString(shared.AttributeIDRequestProtocol); ok {
		request.Protocol = protocol.ToString()
	}
	for _, header := range headers.GetAll() {
		request.Headers = append(request.Headers, [2]string{header[0].ToString(), header[1].ToString()})
	}
	f.result = f.prepared.Execute(request, strings.SplitN(request.URI, "?", 2)[0])
	f.evaluated = true
	r := f.result
	if f.export != nil {
		if err := carrier.Publish(f.handle, *f.export, r.OutcomeRecord()); err != nil {
			f.handle.SendLocalResponse(500, nil, []byte(`{"error":"handoff_failed"}`), "fig_handoff_failed")
			return shared.HeadersStatusStop
		}
	}
	f.handle.SetMetadata("fig.waf", "outcome", map[string]any{"policy": r.Policy, "action": r.Outcome.Action, "matched": r.Outcome.Matched, "rule_id": r.Outcome.RuleID, "generation": r.Generation, "scope": r.Scope})
	if r.Status != 0 {
		body, _ := json.Marshal(map[string]string{"error": r.Code, "action": r.Outcome.Action, "policy": r.Policy})
		f.handle.SendLocalResponse(uint32(r.Status), f.fields(), body, "fig_pipeline_"+r.Code)
		return shared.HeadersStatusStop
	}
	headers.Set("x-fig-policy", r.Policy)
	return shared.HeadersStatusContinue
}
func (f *wafAppFilter) fields() [][2]string {
	r := f.result
	return [][2]string{{"content-type", "application/json"}, {"x-fig-waf-policy", r.Policy}, {"x-fig-waf-action", r.Outcome.Action},
		{"x-fig-waf-matched", strconv.FormatBool(r.Outcome.Matched)}, {"x-fig-waf-rule", strconv.Itoa(r.Outcome.RuleID)}}
}
func (f *wafAppFilter) OnResponseHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if f.evaluated {
		for _, field := range f.fields()[1:] {
			headers.Set(field[0], field[1])
		}
	}
	return shared.HeadersStatusContinue
}

func (f *wafAppFilter) OnStreamComplete() { f.ended = true }
