package filter

import (
	"encoding/json"
	"github.com/dio/fig/bundle"
)

// The common bootstrap wrapper carries data; app compilers own its semantics.
type appConfig struct {
	Entry  bundle.Ref      `json:"entry"`
	Bundle json.RawMessage `json:"bundle"`
}

func decodeAppConfig(data []byte) (appConfig, error) {
	var config appConfig
	err := bundle.DecodeSpec(data, &config)
	return config, err
}
