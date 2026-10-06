// Package match is an experimental, host-independent extraction and selection core.
package match

import (
	"encoding/json"

	"github.com/dio/fig/body"
)

type Phase string

const (
	Headers      Phase = "request-headers"
	BodyComplete Phase = "request-body-complete"
)

func (p Phase) rank() int {
	switch p {
	case Headers:
		return 1
	case BodyComplete:
		return 2
	default:
		return 0
	}
}

type Kind string

const (
	String  Kind = "string"
	Boolean Kind = "boolean"
	Integer Kind = "integer"
)

// Value has one typed payload. Constructors avoid ambiguous zero values.
type Value struct {
	Kind Kind   `json:"kind"`
	Text string `json:"text,omitempty"`
	Bool bool   `json:"bool,omitempty"`
	Int  int64  `json:"int,omitempty"`
}

func Text(s string) Value { return Value{Kind: String, Text: s} }
func Bool(b bool) Value   { return Value{Kind: Boolean, Bool: b} }
func Int(n int64) Value   { return Value{Kind: Integer, Int: n} }
func (v Value) valid() bool {
	switch v.Kind {
	case String:
		return !v.Bool && v.Int == 0
	case Boolean:
		return v.Text == "" && v.Int == 0
	case Integer:
		return v.Text == "" && !v.Bool
	default:
		return false
	}
}

type FactSpec struct {
	Name      string          `json:"name"`
	Extractor string          `json:"extractor"`
	Type      Kind            `json:"type"`
	Args      json.RawMessage `json:"args"`
}

// Predicate is a tagged union. Preparation rejects fields unrelated to its op.
type Predicate struct {
	Op       string      `json:"op"`
	Fact     string      `json:"fact,omitempty"`
	Values   []Value     `json:"values,omitempty"`
	Children []Predicate `json:"children,omitempty"`
}

type Rule struct {
	ID     string          `json:"id"`
	When   Predicate       `json:"when"`
	Result json.RawMessage `json:"result"`
}

type Spec struct {
	Schema   string     `json:"schema"`
	Name     string     `json:"name"`
	Revision string     `json:"revision"`
	Phase    Phase      `json:"phase"`
	Facts    []FactSpec `json:"facts"`
	Rules    []Rule     `json:"rules"`
	// Nil means NoMatch; a non-nil default must validate as the output type.
	Default json.RawMessage `json:"default,omitempty"`
}

type FactState string

const (
	Pending FactState = "pending"
	Present FactState = "present"
	Missing FactState = "missing"
	Invalid FactState = "invalid"
)

type Fact struct {
	State FactState
	Value Value
	Code  string
}

// Input is a complete snapshot at Phase, not a delta. The caller owns buffering.
// Fields are adapter-provided facts, not automatically trusted identity.
// Do not mutate an Input concurrently with Advance. No input buffers are retained.
type Input struct {
	Phase    Phase
	Fields   map[string]Value
	Body     []byte
	Document *body.Document
}

type Status string

const (
	Waiting   Status = "waiting"
	Selected  Status = "selected"
	NoMatch   Status = "no-match"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Result[T any] struct {
	Status     Status
	Value      T
	RuleID     string
	Default    bool
	Code       string
	View       string
	Revision   string
	Generation string
}
