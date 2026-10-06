package main

import (
	"encoding/json"
	"strconv"
	"strings"

	wafapp "github.com/dio/fig/spike/envoy/apps/waf"
	"github.com/dio/fig/spike/envoy/apps/waf/inspect"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

type wafAppConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}
type wafAppFactory struct {
	shared.EmptyHttpFilterFactory
	prepared *wafapp.Prepared
}

func (*wafAppConfigFactory) Create(_ shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	bootstrap, err := decodeAppConfig(data)
	if err != nil {
		return nil, err
	}
	prepared, err := wafapp.Compile(bootstrap.Bundle, bootstrap.Entry)
	if err != nil {
		return nil, err
	}
	return &wafAppFactory{prepared: prepared}, nil
}
func (f *wafAppFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &wafAppFilter{prepared: f.prepared, handle: h}
}

type wafAppFilter struct {
	shared.EmptyHttpFilter
	prepared  *wafapp.Prepared
	handle    shared.HttpFilterHandle
	result    wafapp.Result
	evaluated bool
}

func (f *wafAppFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
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
