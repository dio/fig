// Package control owns Marker configuration actions; it has no host side effects.
package control

import (
	"encoding/json"
	"fmt"

	"github.com/dio/fig/action"
	"github.com/dio/fig/apps/marker"
	"github.com/dio/fig/bundle"
	matchconfig "github.com/dio/fig/match/config"
)

type SetValue struct{}

func (SetValue) Descriptor() action.Descriptor {
	return action.Descriptor{ID: "marker:SetValue", Version: "v1alpha1", Summary: "Set the literal result of an existing Marker Match rule", Effect: "mutate", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["match","rule","value"],"properties":{"match":{"type":"string","minLength":1},"rule":{"type":"string","minLength":1},"value":{"type":"string","pattern":"^[A-Za-z0-9_.-]{1,128}$"}}}`)}
}
func (SetValue) Apply(c action.Config, data json.RawMessage) (action.Change, error) {
	var in struct {
		Match string `json:"match"`
		Rule  string `json:"rule"`
		Value string `json:"value"`
	}
	if err := bundle.DecodeSpec(data, &in); err != nil {
		return action.Change{}, err
	}
	if in.Match == "" || in.Rule == "" || in.Value == "" {
		return action.Change{}, fmt.Errorf("match, rule and value required")
	}
	if _, err := marker.CompileWithInputs(c.Bundle, c.Entry, c.Inputs); err != nil {
		return action.Change{}, err
	}
	b, err := bundle.Decode(c.Bundle)
	if err != nil {
		return action.Change{}, err
	}
	var ref bundle.Ref
	for _, r := range b.Resources() {
		if r.Type == matchconfig.ResourceType && r.Name == in.Match {
			ref = r.Ref
		}
	}
	changed, err := action.Edit(c, ref, func(raw json.RawMessage) (json.RawMessage, error) {
		var spec map[string]json.RawMessage
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, err
		}
		var rules []map[string]json.RawMessage
		if err := json.Unmarshal(spec["rules"], &rules); err != nil {
			return nil, err
		}
		found := false
		for _, rule := range rules {
			var id string
			if err := json.Unmarshal(rule["id"], &id); err != nil {
				return nil, err
			}
			if id == in.Rule {
				found = true
				rule["result"], _ = json.Marshal(map[string]string{"value": in.Value})
			}
		}
		if !found {
			return nil, fmt.Errorf("rule not found")
		}
		spec["rules"], _ = json.Marshal(rules)
		return json.Marshal(spec)
	})
	if err != nil {
		return action.Change{}, err
	}
	if _, err := marker.CompileWithInputs(changed.Config.Bundle, changed.Config.Entry, changed.Config.Inputs); err != nil {
		return action.Change{}, err
	}
	changed.Summary = fmt.Sprintf("match %s rule %s value -> %s", in.Match, in.Rule, in.Value)
	return changed, nil
}
