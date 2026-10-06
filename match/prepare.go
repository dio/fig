package match

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type compiledFact struct {
	name    string
	kind    Kind
	extract Extractor
}
type compiledRule struct {
	id        string
	predicate func(map[string]Fact) bool
	result    json.RawMessage
}

// Prepared owns its compiled configuration; publishing/replacing it is external.
// T must be an ordinary JSON data type, without custom unmarshaling side effects.
type Prepared[T any] struct {
	name, revision string
	phase          Phase
	facts          []compiledFact
	rules          []compiledRule
	fallback       json.RawMessage
}

// Prepare validates every output, including the default. validate can resolve
// scoped references against the caller's generation; it must not publish state.
func Prepare[T any](spec Spec, registry Registry, validate func(T) error) (*Prepared[T], error) {
	if spec.Schema != "fig.match/v1" || spec.Name == "" || spec.Revision == "" {
		return nil, fmt.Errorf("schema, name and revision required")
	}
	if spec.Phase.rank() == 0 {
		return nil, fmt.Errorf("unsupported phase %q", spec.Phase)
	}
	if validate == nil {
		return nil, fmt.Errorf("output validator required")
	}
	p := &Prepared[T]{name: spec.Name, revision: spec.Revision, phase: spec.Phase,
		facts: []compiledFact{}, rules: []compiledRule{}}
	types := map[string]Kind{}
	for _, fact := range spec.Facts {
		if fact.Name == "" {
			return nil, fmt.Errorf("empty fact name")
		}
		if _, ok := types[fact.Name]; ok {
			return nil, fmt.Errorf("duplicate fact %q", fact.Name)
		}
		if !(Value{Kind: fact.Type}).valid() {
			return nil, fmt.Errorf("unknown fact type %q", fact.Type)
		}
		factory := registry[fact.Extractor]
		if factory == nil {
			return nil, fmt.Errorf("unknown extractor %q", fact.Extractor)
		}
		// Keep registry implementations from retaining caller-owned argument bytes.
		fact.Args = bytes.Clone(fact.Args)
		phase, extract, err := factory(fact)
		if err != nil {
			return nil, fmt.Errorf("fact %q: %w", fact.Name, err)
		}
		if extract == nil || phase.rank() == 0 || phase.rank() > spec.Phase.rank() {
			return nil, fmt.Errorf("fact %q unavailable at evaluation phase", fact.Name)
		}
		types[fact.Name] = fact.Type
		p.facts = append(p.facts, compiledFact{name: fact.Name, kind: fact.Type, extract: extract})
	}
	ids := map[string]bool{}
	for _, rule := range spec.Rules {
		if rule.ID == "" || ids[rule.ID] {
			return nil, fmt.Errorf("empty or duplicate rule ID %q", rule.ID)
		}
		ids[rule.ID] = true
		predicate, err := compilePredicate(rule.When, types, 0)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.ID, err)
		}
		result, err := prepareOutput(rule.Result, validate)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.ID, err)
		}
		p.rules = append(p.rules, compiledRule{id: rule.ID, predicate: predicate, result: result})
	}
	if spec.Default != nil {
		output, err := prepareOutput(spec.Default, validate)
		if err != nil {
			return nil, fmt.Errorf("default: %w", err)
		}
		p.fallback = output
	}
	return p, nil
}

func prepareOutput[T any](raw json.RawMessage, validate func(T) error) (json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("null output")
	}
	var value T
	if err := decode(raw, &value); err != nil {
		return nil, err
	}
	if err := validate(value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func compilePredicate(p Predicate, types map[string]Kind, depth int) (func(map[string]Fact) bool, error) {
	if depth > 32 {
		return nil, fmt.Errorf("predicate depth limit")
	}
	switch p.Op {
	case "all", "any", "not":
		if p.Fact != "" || len(p.Values) != 0 || len(p.Children) == 0 {
			return nil, fmt.Errorf("invalid boolean predicate")
		}
		if p.Op == "not" && len(p.Children) != 1 {
			return nil, fmt.Errorf("not requires one child")
		}
		children := []func(map[string]Fact) bool{}
		for _, child := range p.Children {
			fn, err := compilePredicate(child, types, depth+1)
			if err != nil {
				return nil, err
			}
			children = append(children, fn)
		}
		op := p.Op
		return func(facts map[string]Fact) bool {
			if op == "not" {
				return !children[0](facts)
			}
			for _, child := range children {
				value := child(facts)
				if op == "all" && !value {
					return false
				}
				if op == "any" && value {
					return true
				}
			}
			return op == "all"
		}, nil
	case "equals", "in", "exists", "isMissing":
		kind, ok := types[p.Fact]
		if !ok || len(p.Children) != 0 {
			return nil, fmt.Errorf("unknown fact or unexpected children")
		}
		switch p.Op {
		case "equals":
			if len(p.Values) != 1 {
				return nil, fmt.Errorf("equals requires one value")
			}
		case "in":
			if len(p.Values) == 0 {
				return nil, fmt.Errorf("in requires values")
			}
		default:
			if len(p.Values) != 0 {
				return nil, fmt.Errorf("presence predicate takes no values")
			}
		}
		values := append([]Value{}, p.Values...)
		for _, value := range values {
			if !value.valid() || value.Kind != kind {
				return nil, fmt.Errorf("predicate type mismatch")
			}
		}
		name, op := p.Fact, p.Op
		return func(facts map[string]Fact) bool {
			fact := facts[name]
			if op == "isMissing" {
				return fact.State == Missing
			}
			if fact.State != Present {
				return false
			}
			if op == "exists" {
				return true
			}
			for _, value := range values {
				if fact.Value == value {
					return true
				}
			}
			return false
		}, nil
	default:
		return nil, fmt.Errorf("unknown predicate %q", p.Op)
	}
}
