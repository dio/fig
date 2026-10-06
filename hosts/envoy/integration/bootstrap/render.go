// Package bootstrap embeds caller-selected app bundles into an Envoy fixture.
// It has no knowledge of WAF, Marker, or their resource/output schemas.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/handoff"
)

type Binding struct {
	Export *handoff.Slot
	Input  *handoff.Input
	Name   string
	Entry  bundle.Ref
	Data   json.RawMessage
}

func Render(template []byte, bindings ...Binding) ([]byte, error) {
	// Validate the declared ports in template order, not caller argument order.
	ordered := append([]Binding{}, bindings...)
	position := func(b Binding) int {
		marker, _ := json.Marshal("__FIG_" + b.Name + "_BUNDLE_CONFIG__")
		return bytes.Index(template, marker)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return position(ordered[i]) < position(ordered[j]) })
	stages := []handoff.Stage{}
	for _, b := range ordered {
		stages = append(stages, handoff.Stage{Export: b.Export, Input: b.Input})
	}
	if err := handoff.ValidateChain(stages); err != nil {
		return nil, err
	}
	rendered := bytes.Clone(template)
	for _, binding := range bindings {
		marker, _ := json.Marshal("__FIG_" + binding.Name + "_BUNDLE_CONFIG__")
		if bytes.Count(rendered, marker) != 1 {
			return nil, fmt.Errorf("expected one %s bundle marker", binding.Name)
		}
		config, err := json.Marshal(struct {
			Export *handoff.Slot   `json:"export,omitempty"`
			Input  *handoff.Input  `json:"input,omitempty"`
			Entry  bundle.Ref      `json:"entry"`
			Bundle json.RawMessage `json:"bundle"`
		}{Entry: binding.Entry, Bundle: binding.Data, Export: binding.Export, Input: binding.Input})
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(string(config))
		if err != nil {
			return nil, err
		}
		rendered = bytes.Replace(rendered, marker, encoded, 1)
	}
	return rendered, nil
}
