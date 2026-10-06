// Package config bridges serializable bundle resources to the Match primitive.
// It contains no app result types, policies, effects, or Envoy dependencies.
package config

import (
	"encoding/json"
	"fmt"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
)

const ResourceType = "fig.match/v1alpha1"

type spec struct {
	Phase      match.Phase      `json:"phase"`
	OutputType string           `json:"outputType"`
	Facts      []match.FactSpec `json:"facts"`
	Rules      []match.Rule     `json:"rules"`
	OnNoMatch  struct {
		Return string          `json:"return,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
	} `json:"onNoMatch"`
}

// PrepareHeaders compiles the current host contract: string path/method/authority
// facts at request headers. The caller owns output type and semantic validation.
func PrepareHeaders[T any](resource bundle.Resource, outputType string, validate func(T) error) (*match.Prepared[T], error) {
	return PrepareHeadersWithInputs(resource, outputType, validate, nil)
}

// PrepareHeadersWithInputs admits additional fields supplied by a prepared host binding.
// Existing built-in fields cannot be replaced by an input binding.
func PrepareHeadersWithInputs[T any](resource bundle.Resource, outputType string, validate func(T) error, inputs map[string]match.Kind) (*match.Prepared[T], error) {
	fields := map[string]match.Kind{"path": match.String, "method": match.String, "authority": match.String}
	for name, kind := range inputs {
		if len(name) <= 6 || name[:6] != "input." {
			return nil, fmt.Errorf("input namespace required")
		}
		if kind != match.String && kind != match.Boolean && kind != match.Integer {
			return nil, fmt.Errorf("invalid input kind")
		}
		fields[name] = kind
	}
	if resource.Type != ResourceType {
		return nil, fmt.Errorf("expected Match resource")
	}
	var s spec
	if err := bundle.DecodeSpec(resource.Spec, &s); err != nil {
		return nil, err
	}
	if s.Phase != match.Headers || s.OutputType != outputType {
		return nil, fmt.Errorf("header phase and output type %q required", outputType)
	}
	for _, fact := range s.Facts {
		if fact.Extractor != match.InputField {
			return nil, fmt.Errorf("unsupported header extractor")
		}
		var args struct {
			Name string `json:"name"`
		}
		if err := bundle.DecodeSpec(fact.Args, &args); err != nil {
			return nil, err
		}
		if fields[args.Name] == "" {
			return nil, fmt.Errorf("host does not supply field %q", args.Name)
		}
		if fields[args.Name] != fact.Type {
			return nil, fmt.Errorf("input field type mismatch")
		}
	}
	hasDefault := s.OnNoMatch.Result != nil
	if hasDefault && s.OnNoMatch.Return != "" || !hasDefault && s.OnNoMatch.Return != "no-match" {
		return nil, fmt.Errorf("explicit onNoMatch result or no-match required")
	}
	return match.Prepare(match.Spec{
		Schema: match.Schema, Name: resource.Name, Revision: resource.Version, Phase: s.Phase,
		Facts: s.Facts, Rules: s.Rules, Default: s.OnNoMatch.Result,
	}, match.Builtins(), validate)
}
