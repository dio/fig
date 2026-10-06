// Package handoff defines bounded scalar records shared between request-local modules.
package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"

	"github.com/dio/fig/body"
	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
)

const MaxBytes = 4096
const MaxSlots = 16
const Version = "fig.handoff/v1alpha1"

var (
	ErrInvalid  = errors.New("invalid_handoff")
	ErrMismatch = errors.New("handoff_mismatch")
	ErrLimit    = errors.New("handoff_limit")
	ErrMissing  = errors.New("missing_handoff")
	ErrConflict = errors.New("handoff_conflict")
	ErrStore    = errors.New("handoff_store_failed")
)

// Slot is an exact producer contract supplied by trusted composition configuration.
// Generation belongs to the producer, not to the consuming app.
type Slot struct {
	Activation     string      `json:"activation"`
	Placement      string      `json:"placement"`
	Producer       string      `json:"producer"`
	Name           string      `json:"slot"`
	Type           string      `json:"type"`
	Scope          string      `json:"scope"`
	Generation     string      `json:"generation"`
	Representation string      `json:"representation"`
	Phase          match.Phase `json:"phase"`
}

var token = regexp.MustCompile(`^[A-Za-z0-9_.:/@-]{1,128}$`)

func (s Slot) Validate() error {
	for _, v := range []string{s.Activation, s.Placement, s.Producer, s.Name, s.Type, s.Scope, s.Generation, s.Representation} {
		if !token.MatchString(v) {
			return ErrInvalid
		}
	}
	if s.Phase != match.Headers {
		return ErrInvalid
	}
	return nil
}

// Key deliberately excludes generation/type: incompatible writers collide instead
// of creating a second apparent producer for the same placement slot.
func (s Slot) Key() string {
	data, _ := json.Marshal([]string{s.Placement, s.Producer, s.Name})
	sum := sha256.Sum256(data)
	return "fig.handoff." + hex.EncodeToString(sum[:])
}

type Record map[string]match.Value
type envelope struct {
	Version string `json:"version"`
	Slot    Slot   `json:"identity"`
	Values  Record `json:"values"`
}

func validRecord(r Record) bool {
	if len(r) == 0 || len(r) > 16 {
		return false
	}
	for k, v := range r {
		if !token.MatchString(k) {
			return false
		}
		switch v.Kind {
		case match.String:
			if v.Bool || v.Int != 0 || len(v.Text) > 1024 || !utf8.ValidString(v.Text) {
				return false
			}
		case match.Boolean:
			if v.Text != "" || v.Int != 0 {
				return false
			}
		case match.Integer:
			if v.Text != "" || v.Bool {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func Encode(s Slot, r Record) ([]byte, error) {
	if s.Validate() != nil || !validRecord(r) {
		return nil, ErrInvalid
	}
	data, err := json.Marshal(envelope{Version: Version, Slot: s, Values: r})
	if err != nil {
		return nil, ErrInvalid
	}
	if len(data) > MaxBytes {
		return nil, ErrLimit
	}
	return data, nil
}
func Decode(s Slot, data []byte) (Record, error) {
	if len(data) > MaxBytes {
		return nil, ErrLimit
	}
	if s.Validate() != nil {
		return nil, ErrInvalid
	}
	if _, err := body.Parse(data, body.Limits{MaxBytes: MaxBytes, MaxDepth: 8, MaxNodes: 256}); err != nil {
		return nil, ErrInvalid
	}
	var e envelope
	if err := bundle.DecodeSpec(data, &e); err != nil {
		return nil, ErrInvalid
	}
	if e.Version != Version || !validRecord(e.Values) {
		return nil, ErrInvalid
	}
	if e.Slot != s {
		return nil, ErrMismatch
	}
	return e.Values, nil
}

// Input projects one field into the consuming app's Match inputs.
// The input.* namespace prevents replacing native path/method/authority fields.
type Input struct {
	Slot     Slot       `json:"binding"`
	Field    string     `json:"field"`
	Fact     string     `json:"fact"`
	Kind     match.Kind `json:"kind"`
	Required bool       `json:"required"`
}

func (i Input) Validate() error {
	if i.Slot.Validate() != nil || !token.MatchString(i.Field) || !token.MatchString(i.Fact) {
		return ErrInvalid
	}
	if len(i.Fact) <= 6 || i.Fact[:6] != "input." {
		return ErrInvalid
	}
	if i.Kind != match.String && i.Kind != match.Boolean && i.Kind != match.Integer {
		return ErrInvalid
	}
	return nil
}
func (i Input) Project(r Record) (match.Value, error) {
	v, ok := r[i.Field]
	if !ok || v.Kind != i.Kind {
		return match.Value{}, ErrInvalid
	}
	return v, nil
}

// Stage describes only this slice's handoff ports in downstream request order.
// It is not a general app registry or placement planner.
type Stage struct {
	Export *Slot
	Input  *Input
}

func ValidateChain(stages []Stage) error {
	if len(stages) > MaxSlots {
		return ErrLimit
	}
	seen := map[string]Slot{}
	declared := map[string]bool{}
	for _, stage := range stages {
		if stage.Export != nil {
			declared[stage.Export.Key()] = true
		}
	}
	for _, stage := range stages {
		if stage.Input != nil {
			i := *stage.Input
			if err := i.Validate(); err != nil {
				return err
			}
			producer, ok := seen[i.Slot.Key()]
			if !ok && (i.Required || declared[i.Slot.Key()]) {
				return ErrMissing
			}
			if ok && producer != i.Slot {
				return ErrMismatch
			}
		}
		if stage.Export != nil {
			s := *stage.Export
			if err := s.Validate(); err != nil {
				return err
			}
			if _, ok := seen[s.Key()]; ok {
				return ErrConflict
			}
			seen[s.Key()] = s
		}
	}
	return nil
}
