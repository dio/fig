# Fig

Concepts for serializable gateway fact extraction, selection, prepared views and
execution across extensible processing components. Examples include MCP profile/tool
routing, Jev decisions, caching, guardrails (including bring-your-own), LLM routing,
API access and WAF inspection.

- [Design](docs/DESIGN.md): responsibilities, semantics, invariants and examples.
- [Configuration draft](docs/CONFIG.md): consumed bundles, primitive/app boundaries, and next actions.
- [Match primitive](docs/primitives/MATCH.md): specification, preparation, evaluation and host boundaries.
- [Request data](docs/REQUEST_DATA.md): typed request handoffs and [source survey](docs/surveys/REQUEST_DATA.md).
- [Local app actions](docs/LOCAL_ACTIONS.md): discoverable actions and interactive native demo.
- [Rationale](docs/RATIONALE.md): source findings, tradeoffs and open decisions.
- [BoE Coraza WAF case study](docs/case-studies/BOE-CORAZA-WAF.md): reuse boundaries, migration scope and effort estimate.

Apps contribute composable modules with explicit downstream, upstream, or host-selection
placement and typed handoff requirements.

The experimental [Match primitive](match/README.md) implements preparation, typed
fact extraction and selection. Use `make test` for all modules. The broader runtime
remains a design proposal; APIs and schemas are not stable.

## Interactive local runtime

Follow [the two-terminal walkthrough](docs/demo.md) to start one Envoy, discover
`waf:SetMode` / `marker:SetValue`, preview changes, and verify their effect on traffic.

```sh
make build
./bin/fig init --dir /tmp/fig
./bin/fig serve --dir /tmp/fig up --envoy "$HOME/.tetrate/bin/envoy"
```

Changed configurations restart Envoy; no-op actions preserve its PID. Configuration
persists in the private state directory. Apps own schemas and transformations; the
supervisor owns complete-chain activation and regenerates dependent handoff bindings.

## Single-Envoy native tests — no Docker

Requires Go 1.27.1, a cgo compiler, and a matching native Envoy binary. On macOS
arm64, use the `0a804c57` / 1.40.0-dev build:

```sh
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

This builds the host-native module and starts one Envoy child process from Go tests,
with an in-process HTTP backend. Fig imports the published, pinned `dio/kona/envoytest`
runner; no sibling checkout is required. See [setup, binary download, and the native
workflow](docs/LOCAL_DEVELOPMENT.md#native-development-loop-no-docker).

See the [walkthrough and qualification results](docs/LOCAL_DEVELOPMENT.md) for the
local runtime pin and SDK limitation.

The native fixture now includes a minimal Coraza WAF stage: selected policy → header
inspection → continue/block/error, including detection-only mode. See the
[WAF cases](docs/LOCAL_DEVELOPMENT.md#bundle-and-execution).

The WAF app now consumes [the bundle](examples/config/waf.json) during Envoy config
creation. `bundle` owns decoding/references, `match` remains a primitive, and
`apps/waf` owns app composition and policy semantics. Invalid dependencies
are rejected before activation.

## Repository layout

Current execution focus: one local native Envoy. EG, Kubernetes and remote delivery
remain design topics and are not required by the development loop.

```text
action/                     app-owned configuration action contracts
body/                       bounded immutable JSON views
bundle/                     strict envelope decoding and exact references
handoff/                    bounded typed request records and bindings
match/                      fact extraction and selection
match/config/                serializable Match resource preparation
apps/                       independently versioned app modules
  marker/                   Match → diagnostic marker
  waf/                      Match → policy → Coraza inspection
  marker/envoy/             independent Envoy adapter module
  waf/envoy/                independent Envoy adapter module
hosts/envoy/                separate Go module: Envoy SDK and host integration
  cmd/fig-envoy/             c-shared library entry point
  cmd/fig/                   local runtime composition root
  bootstrap/                 shared fixed-chain renderer
  internal/local/            supervisor, snapshots and generic control client
  internal/filter/           standalone Match demo; no app imports
  handoff/                  independent SDK carrier module; no app dependencies
  integration/              native traffic tests, bootstrap and test backend
examples/config/            consumed app bundles
docs/LOCAL_DEVELOPMENT.md   local workflow and qualification
```

Dependencies flow from the executable → app adapters → app logic → core. Core has
no third-party dependencies; app logic has no Envoy SDK dependency. Each Envoy adapter
is a separate nested Go module with its own `go.mod` and `go.sum`. Only the executable
composition root imports concrete app adapters; shared host code has no app imports.
The nested modules isolate dependencies, not runtime processes. They use relative
replacements within this checkout; no sibling repository or Go workspace is needed.
Moving the experimental packages changes their Go import paths; configuration type
names and registered Envoy factory names are unchanged.

`make test` runs tests across all seven modules (native traffic tests skip without
their environment). `make native-test ENVOY_BIN=/path/to/envoy` additionally builds
the matching shared library and runs real local Envoy traffic. Running `go test ./...`
alone at the repository root covers only the core module.

The local Envoy SDK pin uses standard `go.mod`/`go.sum` files. There are no alternate
`native.mod`/`native.sum` manifests or `-modfile` build flags.

The native handoff fixture passes WAF inspection outcomes through bounded filter state
to Marker Match inputs. See [the implemented contract](docs/REQUEST_DATA.md#implemented-configuration-and-admission-boundary)
for configuration, ownership and current limits.
