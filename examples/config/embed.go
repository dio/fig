// Package config provides the canonical local demo bundles.
package config

import _ "embed"

//go:embed waf.json
var WAF []byte

//go:embed marker-inspection.json
var MarkerInspection []byte
