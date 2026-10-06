// Package marker is a diagnostic app composed from Match. It has no dependency on
// WAF, Coraza, or Envoy; selecting a marker has no authorization meaning.
package marker

import (
	"fmt"
	"regexp"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
	matchconfig "github.com/dio/fig/match/config"
)

const (
	AppType      = "fig.marker/v1alpha1"
	OutputType   = "fig.marker-value/v1alpha1"
	PipelineType = "fig.pipeline/v1alpha1"
)

type value struct {
	Value string `json:"value"`
}
type localReply struct {
	Status int `json:"status"`
}
type action struct {
	Continue   bool        `json:"continue,omitempty"`
	LocalReply *localReply `json:"localReply,omitempty"`
}
type handoff struct {
	From string `json:"from"`
	Type string `json:"type"`
}
type step struct {
	ID        string      `json:"id"`
	Module    string      `json:"module"`
	ConfigRef *bundle.Ref `json:"configRef,omitempty"`
	Input     *handoff    `json:"input,omitempty"`
	OnNoMatch *action     `json:"onNoMatch,omitempty"`
	OnError   *action     `json:"onError"`
}
type spec struct {
	App       string `json:"app"`
	Placement string `json:"placement"`
	Steps     []step `json:"steps"`
}
type Prepared struct {
	scope, generation string
	matcher           *match.Prepared[value]
	errorStatus       int
}
type Result struct {
	Scope, Generation, Value, Action string
	Status                           int
}

var markerValue = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func Compile(data []byte, entry bundle.Ref) (*Prepared, error) {
	return CompileWithInputs(data, entry, nil)
}

// CompileWithInputs adds only fields admitted by the host's prepared input bindings.
func CompileWithInputs(data []byte, entry bundle.Ref, inputs map[string]match.Kind) (*Prepared, error) {
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
	if chain.App != AppType || chain.Placement != "downstream-http" || len(chain.Steps) != 2 {
		return nil, fmt.Errorf("supported Marker pipeline: downstream header Match then apply")
	}
	first, last := chain.Steps[0], chain.Steps[1]
	if first.ID == "" || last.ID == "" || first.ID == last.ID {
		return nil, fmt.Errorf("invalid step IDs")
	}
	validMatch := first.Module == matchconfig.ResourceType && first.ConfigRef != nil && first.Input == nil
	validApply := last.Module == "fig.marker.apply/v1alpha1" && last.ConfigRef == nil && last.Input != nil && last.OnNoMatch == nil && last.OnError == nil
	if !validMatch || !validApply {
		return nil, fmt.Errorf("invalid Marker wiring")
	}
	if first.ConfigRef.Type != matchconfig.ResourceType || last.Input.From != first.ID || last.Input.Type != OutputType {
		return nil, fmt.Errorf("invalid Marker handoff")
	}
	if first.OnNoMatch == nil || !first.OnNoMatch.Continue || first.OnNoMatch.LocalReply != nil {
		return nil, fmt.Errorf("Marker no-match must explicitly continue without a marker")
	}
	if !validError(first.OnError) {
		return nil, fmt.Errorf("Marker Match errors require terminal 5xx mapping")
	}
	p := &Prepared{scope: b.Scope(), generation: b.Generation(), errorStatus: first.OnError.LocalReply.Status}
	for _, resource := range b.Resources() {
		switch resource.Type {
		case matchconfig.ResourceType:
			prepared, err := matchconfig.PrepareHeadersWithInputs(resource, OutputType, func(v value) error {
				if !markerValue.MatchString(v.Value) {
					return fmt.Errorf("marker must be 1..128 ASCII token characters")
				}
				return nil
			}, inputs)
			if err != nil {
				return nil, err
			}
			if resource.Ref == *first.ConfigRef {
				p.matcher = prepared
			}
		case PipelineType:
			if resource.Ref != entry {
				return nil, fmt.Errorf("only one Marker pipeline per bundle supported")
			}
		default:
			return nil, fmt.Errorf("unregistered Marker resource type %q", resource.Type)
		}
	}
	if p.matcher == nil {
		return nil, fmt.Errorf("unresolved Marker Match reference")
	}
	return p, nil
}
func validError(a *action) bool {
	return a != nil && !a.Continue && a.LocalReply != nil && a.LocalReply.Status >= 500 && a.LocalReply.Status <= 599
}
func (p *Prepared) Execute(fields map[string]match.Value) Result {
	r := Result{Scope: p.scope, Generation: p.generation, Action: "continue"}
	selected := p.matcher.Begin(p.generation).Advance(match.Input{Phase: match.Headers, Fields: fields})
	switch selected.Status {
	case match.Selected:
		r.Value = selected.Value.Value
	case match.NoMatch:
		r.Action = "skip"
	default:
		r.Action = "error"
		r.Status = p.errorStatus
	}
	return r
}
