# Fig

Concepts for serializable gateway fact extraction, selection, prepared views and
execution across extensible processing components. Examples include MCP profile/tool
routing, Jev decisions, caching, guardrails (including bring-your-own), LLM routing,
API access and WAF inspection.

- [Design](docs/DESIGN.md): responsibilities, semantics, invariants and examples.
- [Match primitive](docs/primitives/MATCH.md): specification, preparation, evaluation and host boundaries.
- [Rationale](docs/RATIONALE.md): source findings, tradeoffs and open decisions.
- [BoE Coraza WAF case study](docs/case-studies/BOE-CORAZA-WAF.md): reuse boundaries, migration scope and effort estimate.

Apps contribute composable modules with explicit downstream, upstream, or host-selection
placement and typed handoff requirements.

An experimental [Go Match spike](match/README.md) now exercises preparation, typed
fact extraction and selection. Run `go run ./cmd/match-spike`. The broader runtime
remains a design proposal; APIs and schemas are not stable.

## Native Envoy spike

Run Match as two Envoy Go dynamic-module filters against a real backend:

```sh
python3 spike/envoy/verify.py
```

Requires Docker/Compose and Python 3. See the [native walkthrough and qualification
results](spike/envoy/README.md), including manual startup and the pinned SDK limitation.
