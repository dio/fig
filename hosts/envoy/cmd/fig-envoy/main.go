// Command fig-envoy composes app adapters into one local Envoy dynamic module.
package main

import (
	marker "github.com/dio/fig/apps/marker/envoy"
	waf "github.com/dio/fig/apps/waf/envoy"
	"github.com/dio/fig/hosts/envoy/internal/filter"
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

func init() {
	sdk.RegisterHttpFilterConfigFactories(map[string]shared.HttpFilterConfigFactory{
		"fig-match":      filter.NewConfigFactory(),
		"fig-waf-app":    &waf.ConfigFactory{},
		"fig-marker-app": &marker.ConfigFactory{},
	})
}
func main() {}
