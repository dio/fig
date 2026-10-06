# Apps

Each app is an independent Go module:

- `marker`: typed literal selection through Match; depends only on Fig core.
- `waf`: policy selection and header inspection; owns the Coraza dependency.

There is no shared `apps/go.mod`. Package import paths are unchanged by the split.
Marker currently needs no `go.sum`: its only dependency is replaced by local Fig
core, which has no external dependencies. Go will generate checksums if needed.

App logic owns resource schemas, primitive composition and action mappings, without
Envoy handles or deployment topology. Separate `marker/envoy` and `waf/envoy` modules
own callbacks, each importing only its own app. The local Envoy executable registers
both adapters and therefore intentionally includes WAF's Coraza dependencies.

Run `go test -race ./...` inside an individual app module. From the repository root,
`make test` covers all six modules; `make native-test` also exercises local Envoy.
The bundles under root `examples/config` are shared by app and integration tests.
Public APIs remain experimental.
