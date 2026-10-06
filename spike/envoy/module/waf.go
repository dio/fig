package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/corazawaf/coraza/v3"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

type wafPolicy struct {
	Mode string `json:"mode"`
}
type wafConfig struct {
	Selection  string               `json:"selection"`
	Generation string               `json:"generation"`
	Policies   map[string]wafPolicy `json:"policies"`
}
type wafConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}
type wafFactory struct {
	shared.EmptyHttpFilterFactory
	config  wafConfig
	engines map[string]coraza.WAF
}

func (*wafConfigFactory) Create(_ shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	if len(data) > 65536 {
		return nil, fmt.Errorf("WAF config limit")
	}
	var cfg wafConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one WAF configuration")
	}
	if cfg.Selection == "" || cfg.Generation == "" || len(cfg.Policies) == 0 {
		return nil, fmt.Errorf("selection, generation and policies required")
	}
	f := &wafFactory{config: cfg, engines: map[string]coraza.WAF{}}
	for name, policy := range cfg.Policies {
		mode := "On"
		switch policy.Mode {
		case "enforce":
		case "detect":
			mode = "DetectionOnly"
		default:
			return nil, fmt.Errorf("unknown mode %q", policy.Mode)
		}
		// Deliberately fixed smoke rule. No untrusted SecLang, filesystem includes or CRS.
		directives := "SecRuleEngine " + mode + "\n" + `SecRule REQUEST_HEADERS:X-Fig-Attack "@streq attack" "id:1001,phase:1,deny,status:403,nolog"`
		engine, err := coraza.NewWAF(coraza.NewWAFConfig().WithDirectives(directives))
		if err != nil {
			return nil, fmt.Errorf("policy %q: %w", name, err)
		}
		f.engines[name] = engine
	}
	return f, nil
}
func (f *wafFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &wafFilter{factory: f, handle: h}
}

// wafOutcome is component-owned. Match selects a reference; WAF decides next action.
type wafOutcome struct {
	Policy  string `json:"policy"`
	Mode    string `json:"mode"`
	Matched bool   `json:"matched"`
	Action  string `json:"action"`
	RuleID  int    `json:"ruleId,omitempty"`
}
type wafFilter struct {
	shared.EmptyHttpFilter
	factory   *wafFactory
	handle    shared.HttpFilterHandle
	outcome   wafOutcome
	evaluated bool
}

func (f *wafFilter) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	// Selection comes exclusively from host-local metadata, never these client fields.
	for _, name := range []string{"x-fig-waf-action", "x-fig-waf-matched", "x-fig-waf-policy", "x-fig-waf-rule"} {
		headers.Remove(name)
	}
	cfg := f.factory.config
	ref, ok := f.handle.GetMetadataString(shared.MetadataSourceTypeDynamic, "fig.match", cfg.Selection+".ref")
	generation, hasGeneration := f.handle.GetMetadataString(shared.MetadataSourceTypeDynamic, "fig.match", cfg.Selection+".generation")
	if !ok || !hasGeneration || generation.ToString() != cfg.Generation {
		return f.unavailable()
	}
	f.outcome.Policy = ref.ToString()
	engine, ok := f.factory.engines[f.outcome.Policy]
	if !ok {
		return f.unavailable()
	}
	f.outcome.Mode = cfg.Policies[f.outcome.Policy].Mode
	tx := engine.NewTransaction()
	// This adapter only runs phase 1, so transaction ownership ends in this callback.
	// A body/response implementation must instead retain it through stream completion.
	defer func() { tx.ProcessLogging(); _ = tx.Close() }()
	tx.ProcessURI(headers.GetOne(":path").ToString(), headers.GetOne(":method").ToString(), "HTTP/1.1")
	tx.AddRequestHeader("Host", headers.GetOne(":authority").ToString())
	for _, header := range headers.GetAll() {
		name := header[0].ToString()
		if strings.HasPrefix(name, ":") {
			continue
		}
		tx.AddRequestHeader(name, header[1].ToString())
	}
	interruption := tx.ProcessRequestHeaders()
	f.outcome.Matched = len(tx.MatchedRules()) > 0
	if f.outcome.Matched {
		f.outcome.RuleID = 1001
	}
	f.outcome.Action = "continue"
	if interruption != nil {
		f.outcome.Action = "block"
		f.publish()
		data, _ := json.Marshal(map[string]string{"error": "waf_blocked", "policy": f.outcome.Policy, "action": "block"})
		f.handle.SendLocalResponse(403, f.responseFields(), data, "fig_waf_blocked")
		return shared.HeadersStatusStop
	}
	f.publish()
	return shared.HeadersStatusContinue
}
func (f *wafFilter) unavailable() shared.HeadersStatus {
	f.outcome.Action = "error"
	f.publish()
	f.handle.SendLocalResponse(503, f.responseFields(), []byte(`{"error":"waf_policy_unavailable","action":"error"}`), "fig_waf_unavailable")
	return shared.HeadersStatusStop
}
func (f *wafFilter) publish() {
	f.evaluated = true
	f.handle.SetMetadata("fig.waf", "outcome", map[string]any{
		"policy": f.outcome.Policy, "mode": f.outcome.Mode, "matched": f.outcome.Matched, "action": f.outcome.Action, "rule_id": f.outcome.RuleID,
	})
}
func (f *wafFilter) responseFields() [][2]string {
	return [][2]string{{"content-type", "application/json"}, {"x-fig-waf-action", f.outcome.Action},
		{"x-fig-waf-matched", strconv.FormatBool(f.outcome.Matched)}, {"x-fig-waf-policy", f.outcome.Policy},
		{"x-fig-waf-rule", strconv.Itoa(f.outcome.RuleID)}}
}
func (f *wafFilter) OnResponseHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if f.evaluated {
		for _, field := range f.responseFields()[1:] {
			headers.Set(field[0], field[1])
		}
	}
	return shared.HeadersStatusContinue
}
