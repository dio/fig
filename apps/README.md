# Apps

Host-independent app compilers and execution. This separate Go module keeps app
dependencies (currently Coraza) out of the core module.

- `marker`: typed literal selection through Match.
- `waf`: policy selection and header inspection; `waf/inspect` owns Coraza integration.

App logic packages own their resource schemas, primitive composition and action mappings. They do
not register Envoy factories, hold native handles or own deployment topology.
The separate `marker/envoy` and `waf/envoy` Go modules translate callbacks into app
inputs and apply returned actions. Each exports `ConfigFactory` and has its own
`go.mod`/`go.sum`; neither imports `hosts/envoy`. Only the local executable registers
them. The tiny bundle wrapper is decoded privately by each adapter, avoiding a
reverse dependency on the host.

From this directory: `go test -race ./...` tests app logic. Nested adapter modules
are tested separately. From the repository root: `make test` covers all modules.
Bundles under `examples/config` at the repository root are shared by app tests and
the local Envoy integration fixture. Public APIs remain experimental.
