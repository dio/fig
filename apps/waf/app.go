// Package waf is the WAF app: it composes Match selection with policy resolution
// and Coraza inspection. The generic bundle and Match packages do not know this app.
package waf

import (
	"fmt"

	"github.com/dio/fig/apps/waf/inspect"
	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
	"github.com/dio/fig/matchconfig"
)

const (
	MatchType     = "fig.match/v1alpha1"
	PolicyType    = "fig.waf-policy/v1alpha1"
	PipelineType  = "fig.pipeline/v1alpha1"
	SelectionType = "fig.waf-policy-ref/v1alpha1"
)

type selection struct {
	PolicyRef bundle.Ref `json:"policyRef"`
}
type reply struct {
	LocalReply struct {
		Status int `json:"status"`
	} `json:"localReply"`
}
type input struct {
	From string `json:"from"`
	Type string `json:"type"`
}
type step struct {
	ID        string      `json:"id"`
	Module    string      `json:"module"`
	ConfigRef *bundle.Ref `json:"configRef,omitempty"`
	Input     *input      `json:"input,omitempty"`
	OnNoMatch *reply      `json:"onNoMatch,omitempty"`
	OnBlock   *reply      `json:"onBlock,omitempty"`
	OnError   *reply      `json:"onError"`
}
type spec struct {
	App       string `json:"app"`
	Placement string `json:"placement"`
	Steps     []step `json:"steps"`
}
type Prepared struct {
	scope, generation                                            string
	matcher                                                      *match.Prepared[selection]
	policies                                                     map[bundle.Ref]*inspect.Prepared
	noMatchStatus, matchErrorStatus, blockStatus, wafErrorStatus int
}
type Result struct {
	Scope, Generation, Policy string
	Outcome                   inspect.Outcome
	// Status zero continues. Nonzero is a terminal local HTTP response.
	Status int
	Code   string
}

// Compile prepares the complete supported bundle before exposing any executable state.
// The current capability is exactly one header Match followed by one WAF stage.
func Compile(data []byte, entry bundle.Ref) (*Prepared, error) {
	b, err := bundle.Decode(data)
	if err != nil {
		return nil, err
	}
	if entry.Type != PipelineType {
		return nil, fmt.Errorf("entry must reference a pipeline")
	}
	raw, err := b.Resolve(entry)
	if err != nil {
		return nil, err
	}
	var chain spec
	if err := bundle.DecodeSpec(raw, &chain); err != nil {
		return nil, err
	}
	if chain.App != "fig.waf/v1alpha1" || chain.Placement != "downstream-http" || len(chain.Steps) != 2 {
		return nil, fmt.Errorf("supported pipeline: downstream header Match then WAF")
	}
	first, last := chain.Steps[0], chain.Steps[1]
	validMatch := first.Module == MatchType && first.ConfigRef != nil && first.Input == nil && first.OnBlock == nil
	validWAF := last.Module == "fig.waf.inspect/v1alpha1" && last.ConfigRef == nil && last.Input != nil && last.OnNoMatch == nil
	if !validMatch || !validWAF {
		return nil, fmt.Errorf("invalid module wiring")
	}
	if first.ID == "" || last.ID == "" || first.ID == last.ID {
		return nil, fmt.Errorf("invalid step IDs")
	}
	if last.Input.From != first.ID || last.Input.Type != SelectionType {
		return nil, fmt.Errorf("invalid WAF selection handoff")
	}
	if first.ConfigRef.Type != MatchType {
		return nil, fmt.Errorf("expected Match config reference")
	}
	if !validReply(first.OnNoMatch, 400, 599) || !validReply(first.OnError, 400, 599) || !validReply(last.OnBlock, 400, 499) || !validReply(last.OnError, 500, 599) {
		return nil, fmt.Errorf("required terminal error/block mapping missing or invalid")
	}
	p := &Prepared{scope: b.Scope(), generation: b.Generation(), policies: map[bundle.Ref]*inspect.Prepared{},
		noMatchStatus: first.OnNoMatch.LocalReply.Status, matchErrorStatus: first.OnError.LocalReply.Status,
		blockStatus: last.OnBlock.LocalReply.Status, wafErrorStatus: last.OnError.LocalReply.Status}
	resources := b.Resources()
	for _, resource := range resources {
		switch resource.Type {
		case PolicyType:
			var policy inspect.Spec
			if err := bundle.DecodeSpec(resource.Spec, &policy); err != nil {
				return nil, fmt.Errorf("policy %s: %w", resource.Name, err)
			}
			prepared, err := inspect.Prepare(policy)
			if err != nil {
				return nil, fmt.Errorf("policy %s: %w", resource.Name, err)
			}
			p.policies[resource.Ref] = prepared
		case MatchType:
		case PipelineType:
			if resource.Ref != entry {
				return nil, fmt.Errorf("only one pipeline supported in this slice")
			}
		default:
			return nil, fmt.Errorf("unregistered resource type %q", resource.Type)
		}
	}
	for _, resource := range resources {
		if resource.Type != MatchType {
			continue
		}
		prepared, err := p.prepareMatch(resource)
		if err != nil {
			return nil, fmt.Errorf("match %s: %w", resource.Name, err)
		}
		if resource.Ref == *first.ConfigRef {
			p.matcher = prepared
		}
	}
	if p.matcher == nil {
		return nil, fmt.Errorf("unresolved Match reference")
	}
	return p, nil
}
func validReply(r *reply, min, max int) bool {
	return r != nil && r.LocalReply.Status >= min && r.LocalReply.Status <= max
}
func (p *Prepared) prepareMatch(resource bundle.Resource) (*match.Prepared[selection], error) {
	validate := func(value selection) error {
		if value.PolicyRef.Type != PolicyType {
			return fmt.Errorf("wrong policy reference type")
		}
		if _, ok := p.policies[value.PolicyRef]; !ok {
			return fmt.Errorf("unresolved policy reference %s@%s", value.PolicyRef.Name, value.PolicyRef.Version)
		}
		return nil
	}
	return matchconfig.PrepareHeaders(resource, SelectionType, validate)
}
func (p *Prepared) Execute(req inspect.Request, path string) Result {
	result := Result{Scope: p.scope, Generation: p.generation}
	decision := p.matcher.Begin(p.generation).Advance(match.Input{Phase: match.Headers, Fields: map[string]match.Value{
		"path": match.Text(path), "method": match.Text(req.Method), "authority": match.Text(req.Authority),
	}})
	switch decision.Status {
	case match.NoMatch:
		result.Status = p.noMatchStatus
		result.Code = "no_match"
		result.Outcome.Action = "error"
		return result
	case match.Selected:
	default:
		result.Status = p.matchErrorStatus
		result.Code = "invalid_match_input"
		result.Outcome.Action = "error"
		return result
	}
	ref := decision.Value.PolicyRef
	result.Policy = ref.Name
	policy, ok := p.policies[ref]
	if !ok {
		result.Status = p.wafErrorStatus
		result.Code = "policy_unavailable"
		result.Outcome.Action = "error"
		return result
	}
	outcome, err := policy.Inspect(req)
	result.Outcome = outcome
	if err != nil {
		result.Status = p.wafErrorStatus
		result.Code = "inspection_error"
		return result
	}
	if outcome.Action == "block" {
		result.Status = p.blockStatus
		result.Code = "waf_blocked"
	}
	return result
}
