module github.com/dio/fig/hosts/envoy

go 1.27.1

require (
	github.com/corazawaf/coraza/v3 v3.7.0 // indirect
	github.com/corazawaf/libinjection-go v0.3.2 // indirect
	github.com/dio/fig/apps/marker v0.0.0 // indirect
	github.com/dio/fig/apps/waf v0.0.0 // indirect
	github.com/dio/fig/hosts/envoy/handoff v0.0.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/goccy/go-yaml v1.18.0 // indirect
	github.com/gotnospirit/makeplural v0.0.0-20180622080156-a5f48d94d976 // indirect
	github.com/gotnospirit/messageformat v0.0.0-20221001023931-dfe49f1eb092 // indirect
	github.com/kaptinlin/go-i18n v0.1.4 // indirect
	github.com/kaptinlin/jsonschema v0.4.6 // indirect
	github.com/magefile/mage v1.17.0 // indirect
	github.com/petar-dambovaliev/aho-corasick v0.0.0-20250424160509-463d218d4745 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/valllabh/ocsf-schema-golang v1.0.3 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	rsc.io/binaryregexp v0.2.0 // indirect
)

replace github.com/dio/fig => ../..

replace github.com/envoyproxy/envoy/source/extensions/dynamic_modules => github.com/dio/envoy/source/extensions/dynamic_modules v0.0.0-20260909102307-0a804c57cf5f

replace github.com/dio/fig/apps/marker => ../../apps/marker

replace github.com/dio/fig/apps/waf => ../../apps/waf

replace github.com/dio/fig/apps/marker/envoy => ../../apps/marker/envoy

replace github.com/dio/fig/apps/waf/envoy => ../../apps/waf/envoy

require (
	github.com/dio/fig v0.0.0
	github.com/dio/fig/apps/marker/envoy v0.0.0
	github.com/dio/fig/apps/waf/envoy v0.0.0
	github.com/dio/kona v0.0.0-20261006030050-dc14e5541e93
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260909102307-0a804c57cf5f
)

replace github.com/dio/fig/hosts/envoy/handoff => ./handoff
