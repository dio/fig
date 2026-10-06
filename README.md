# Fig

Concepts for serializable gateway fact extraction, selection, prepared views and
execution across extensible processing components. Examples include MCP profile/tool
routing, Jev decisions, caching, guardrails (including bring-your-own), LLM routing,
API access and WAF inspection.

- [Design](docs/DESIGN.md): responsibilities, semantics, invariants and examples.
- [Configuration draft](docs/CONFIG.md): versioned resources, attachments and next actions.
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
workflow](spike/envoy/README.md#direct-native-tests-no-docker).

The earlier Docker Compose lane remains available:

```sh
python3 spike/envoy/verify.py
```

See the [walkthrough and qualification results](spike/envoy/README.md) for the separate
native/Docker pins and SDK limitation.

The native fixture now includes a minimal Coraza WAF stage: selected policy → header
inspection → continue/block/error, including detection-only mode. See the
[WAF cases](spike/envoy/README.md#waf-selection-and-next-action-spike).
