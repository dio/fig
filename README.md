# Fig

Concepts for serializable gateway fact extraction, selection, prepared views and
execution across extensible processing components. Examples include MCP profile/tool
routing, Jev decisions, caching, guardrails (including bring-your-own), LLM routing,
API access and WAF inspection.

- [Design](docs/DESIGN.md): responsibilities, semantics, invariants and examples.
- [Configuration draft](docs/CONFIG.md): consumed bundles, primitive/app boundaries, and next actions.
- [Match primitive](docs/primitives/MATCH.md): specification, preparation, evaluation and host boundaries.
- [Rationale](docs/RATIONALE.md): source findings, tradeoffs and open decisions.
- [BoE Coraza WAF case study](docs/case-studies/BOE-CORAZA-WAF.md): reuse boundaries, migration scope and effort estimate.

Apps contribute composable modules with explicit downstream, upstream, or host-selection
placement and typed handoff requirements.

An experimental [Go Match spike](match/README.md) now exercises preparation, typed
fact extraction and selection. Run `go run ./cmd/match-spike`. The broader runtime
remains a design proposal; APIs and schemas are not stable.

## Single-Envoy native tests — no Docker

Requires Go 1.27.1, a cgo compiler, and a matching native Envoy binary. On macOS
arm64, use the `0a804c57` / 1.40.0-dev build:

```sh
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

This builds the host-native module and starts one Envoy child process from Go tests,
with an in-process HTTP backend. Fig imports the published, pinned `dio/kona/envoytest`
runner; no sibling checkout is required. See [setup, binary download, and the native
workflow](spike/envoy/README.md#native-development-loop-no-docker).

The earlier Docker Compose lane remains available:

```sh
python3 spike/envoy/verify.py
```

See the [walkthrough and qualification results](spike/envoy/README.md) for the separate
native/Docker pins and SDK limitation.

The native fixture now includes a minimal Coraza WAF stage: selected policy → header
inspection → continue/block/error, including detection-only mode. See the
[WAF cases](spike/envoy/README.md#bundle-and-execution).

The WAF app now consumes [the bundle](examples/config/waf.json) during Envoy config
creation. `bundle` owns decoding/references, `match` remains a primitive, and
`apps/waf` owns app composition and policy semantics. Invalid dependencies
are rejected before activation.

## Repository layout

Current execution focus: one local native Envoy. EG, Kubernetes and remote delivery
remain design topics and are not required by the development loop.

```text
body/                       bounded immutable JSON views
bundle/                     strict envelope decoding and exact references
match/                      fact extraction and selection
matchconfig/                serializable Match resource preparation
apps/                       separate Go module: app semantics and dependencies
  marker/                   Match → diagnostic marker
  waf/                      Match → policy → Coraza inspection
hosts/envoy/                separate Go module: Envoy SDK and host integration
  cmd/fig-envoy/             c-shared library entry point
  internal/filter/           callbacks and registered factories
  integration/              native traffic tests, bootstrap and test backend
examples/config/            consumed app bundles
spike/envoy/                walkthrough and fixture launchers
```

Dependencies flow from host → apps → core. Core has no third-party dependencies;
apps have no Envoy SDK dependency. Concrete factory registration stays in the host.
The nested modules isolate dependencies, not runtime processes. They use relative
replacements within this checkout; no sibling repository or Go workspace is needed.
Moving the experimental packages changes their Go import paths; configuration type
names and registered Envoy factory names are unchanged.

`make test` runs tests across all three modules (native traffic tests skip without
their environment). `make native-test ENVOY_BIN=/path/to/envoy` additionally builds
the matching shared library and runs real local Envoy traffic. Running `go test ./...`
alone at the repository root covers only the core module.
