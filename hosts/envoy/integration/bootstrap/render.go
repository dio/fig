// Package bootstrap embeds caller-selected app bundles into an Envoy fixture.
// It has no knowledge of WAF, Marker, or their resource/output schemas.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dio/fig/bundle"
)

type Binding struct {
	Name  string
	Entry bundle.Ref
	Data  json.RawMessage
}

func Render(template []byte, bindings ...Binding) ([]byte, error) {
	rendered := bytes.Clone(template)
	for _, binding := range bindings {
		marker, _ := json.Marshal("__FIG_" + binding.Name + "_BUNDLE_CONFIG__")
		if bytes.Count(rendered, marker) != 1 {
			return nil, fmt.Errorf("expected one %s bundle marker", binding.Name)
		}
		config, err := json.Marshal(struct {
			Entry  bundle.Ref      `json:"entry"`
			Bundle json.RawMessage `json:"bundle"`
		}{Entry: binding.Entry, Bundle: binding.Data})
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
