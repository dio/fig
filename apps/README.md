# Apps

Host-independent app compilers and execution. This separate Go module keeps app
dependencies (currently Coraza) out of the core module.

- `marker`: typed literal selection through Match.
- `waf`: policy selection and header inspection; `waf/inspect` owns Coraza integration.

Apps own their resource schemas, primitive composition and action mappings. They do
not register Envoy factories, hold native handles or own deployment topology.
The Envoy host translates callbacks into app inputs and applies the returned actions.

From this directory: `go test -race ./...`. From the repository root: `make test`.
Bundles under `examples/config` at the repository root are shared by app tests and
the local Envoy integration fixture. Public APIs remain experimental.
