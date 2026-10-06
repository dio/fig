// Command fig-envoy is the c-shared entry point for the Envoy dynamic module.
package main

import (
	"github.com/dio/fig/hosts/envoy/internal/filter"
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
)

func init() { filter.Register() }
func main() {}
