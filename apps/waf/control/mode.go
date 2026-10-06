// Package control owns WAF configuration actions; it has no host side effects.
package control

import (
	"encoding/json"
	"fmt"

	"github.com/dio/fig/action"
	"github.com/dio/fig/apps/waf"
	"github.com/dio/fig/apps/waf/inspect"
	"github.com/dio/fig/bundle"
)

type SetMode struct{}

func (SetMode) Descriptor() action.Descriptor {
	return action.Descriptor{ID: "waf:SetMode", Version: "v1alpha1", Summary: "Set an existing WAF policy mode", Effect: "mutate", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["policy","mode"],"properties":{"policy":{"type":"string","minLength":1},"mode":{"type":"string","enum":["enforce","detect"]}}}`)}
}
func (SetMode) Apply(c action.Config, data json.RawMessage) (action.Change, error) {
	var in struct {
		Policy string `json:"policy"`
		Mode   string `json:"mode"`
	}
	if err := bundle.DecodeSpec(data, &in); err != nil {
		return action.Change{}, err
	}
	if in.Policy == "" || (in.Mode != "enforce" && in.Mode != "detect") {
		return action.Change{}, fmt.Errorf("policy and enforce/detect mode required")
	}
	if _, err := waf.Compile(c.Bundle, c.Entry); err != nil {
		return action.Change{}, err
	}
	b, err := bundle.Decode(c.Bundle)
	if err != nil {
		return action.Change{}, err
	}
	var ref bundle.Ref
	for _, r := range b.Resources() {
		if r.Type == waf.PolicyType && r.Name == in.Policy {
			ref = r.Ref
		}
	}
	changed, err := action.Edit(c, ref, func(raw json.RawMessage) (json.RawMessage, error) {
		var spec inspect.Spec
		if err := bundle.DecodeSpec(raw, &spec); err != nil {
			return nil, err
		}
		spec.Mode = in.Mode
		return json.Marshal(spec)
	})
	if err != nil {
		return action.Change{}, err
	}
	if _, err := waf.Compile(changed.Config.Bundle, changed.Config.Entry); err != nil {
		return action.Change{}, err
	}
	changed.Summary = fmt.Sprintf("policy %s mode -> %s", in.Policy, in.Mode)
	return changed, nil
}
