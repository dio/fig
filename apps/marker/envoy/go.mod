module github.com/dio/fig/apps/marker/envoy

go 1.27.1

replace github.com/dio/fig => ../../..

replace github.com/dio/fig/apps/marker => ..

replace github.com/envoyproxy/envoy/source/extensions/dynamic_modules => github.com/dio/envoy/source/extensions/dynamic_modules v0.0.0-20260909102307-0a804c57cf5f

require (
	github.com/dio/fig v0.0.0
	github.com/dio/fig/apps/marker v0.0.0
	github.com/dio/fig/hosts/envoy/handoff v0.0.0
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260909102307-0a804c57cf5f
)

replace github.com/dio/fig/hosts/envoy/handoff => ../../../hosts/envoy/handoff
