// Package bootstrap embeds a supplied bundle into the Envoy fixture template.
// It performs no semantic preparation; the dynamic module compiles at config creation.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/dio/fig/bundle"
)

func Render(template, data []byte) ([]byte, error) {
	marker := []byte(`"__FIG_BUNDLE_CONFIG__"`)
	if bytes.Count(template, marker) != 1 {
		return nil, fmt.Errorf("expected one bundle marker")
	}
	config, err := json.Marshal(struct {
		Entry  bundle.Ref      `json:"entry"`
		Bundle json.RawMessage `json:"bundle"`
	}{
		Entry: bundle.Ref{Type: "fig.pipeline/v1alpha1", Name: "edge", Version: "1"}, Bundle: data,
	})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(string(config))
	if err != nil {
		return nil, err
	}
	return bytes.Replace(template, marker, encoded, 1), nil
}
