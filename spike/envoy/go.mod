module github.com/dio/fig/spike/envoy

go 1.27.1

require (
	github.com/dio/fig v0.0.0
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260423231439-f1dd21b16c24
)

replace github.com/dio/fig => ../..
