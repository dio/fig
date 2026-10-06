# Local development

The fixture runs a **WAF app composed from Match and Coraza inspection**, followed by
a Marker app and an independent body-Match demonstration, inside one Envoy process.

```text
canonical WAF bundle -> WAF app compiler -> prepared Match + policies + outcome mappings
                                           |
request -> fig-waf-app: Match -> resolve policy -> inspect -> continue / local reply
        -> fig-marker-app: Match -> apply diagnostic value
        -> body Match for /chat -> Envoy router -> echo backend
```

Match is a primitive. WAF owns its policy schema, typed selection output, composition
and inspection semantics. The [configuration design](CONFIG.md) describes
these boundaries and the deliberately limited supported pipeline.

## Native development loop (no Docker)

Requirements: Go 1.27.1, a cgo compiler (Xcode Command Line Tools on macOS), and a
matching native Envoy. This lane pins the `0a804c57` / 1.40.0-dev SDK using `hosts/envoy/go.mod`.
On macOS arm64:

```sh
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

The root target runs core and app tests, builds the host-native shared library,
and runs app/compiler and native traffic tests. `dio/kona/envoytest` starts Envoy and
owns readiness/logging/cleanup. The backend is an in-process Go `httptest.Server`.
No sibling Kona checkout, Compose, Docker or Kubernetes is required.

The suite reads `examples/config/waf.json`, embeds it into the module config and lets
Envoy's config factory call the WAF app compiler. A malformed or unresolved bundle
rejects configuration before serving. Diagnostic client headers cannot choose policy.

Download the matching macOS 15+ arm64 binary if needed:

```sh
mkdir -p .bin
curl -fL --retry 3 \
  https://github.com/dio/envoy-builder/releases/download/envoy-0a804c57-macos15/envoy-darwin-arm64 \
  -o .bin/envoy.download
printf '%s  %s\n' \
  7ca52fc807b9dc0c95303b50ed5bab65df945a2ceb4ebc0440b88377d4fcb76d \
  .bin/envoy.download | shasum -a 256 -c - && \
  chmod +x .bin/envoy.download && mv .bin/envoy.download .bin/envoy
make native-test ENVOY_BIN="$PWD/.bin/envoy"
```

The installed binary was used for qualification; the download recipe was not rerun.
Native suites explicitly skip if `ENVOY_BIN` is unset; the Make target requires it
and checks its commit. Direct test callers must supply a matching module and binary.

## Bundle and execution

The canonical example selects `chat-policy` for `/chat`, `observe-policy` for
`/observe`, and `baseline` otherwise. Policies now contain structured header rules;
the example matches `X-Fig-Attack: attack`, rule 1001.

| Request | Inspection result | Next action |
|---|---|---|
| Clean `/chat` | chat-policy, no match | Continue to body Match |
| `/chat` with attack header | chat-policy, enforce, matched | 403; no backend receipt |
| `/observe` with attack header | observe-policy, detect, matched | Continue |
| `/headers` with attack header | baseline, enforce, matched | 403; no backend receipt |

A bad policy reference now rejects the bundle; the old `/unconfigured` request-time
503 fixture was removed. A runtime missing-policy fallback remains fail closed, but
normal compiled configuration cannot select an unresolved policy.

The app emits typed results internally and `fig.waf` dynamic metadata for observation.
Response headers show `x-fig-waf-policy`, `x-fig-waf-action`, `x-fig-waf-matched`, and
`x-fig-waf-rule`. `x-fig-policy` is a diagnostic upstream header, not a trust channel.
Each header-only inspection transaction is finalized/closed in its callback. Body or
response inspection would require a different retained transaction lifecycle.

The separate body-Match fixture still handles `/chat` JSON model selection with a
4096-byte limit and returns 404/400/413/415 for no-match/invalid/oversized/unsupported
input. It has not been folded into the header-only WAF app or its activation boundary.
No provider routing, actual LLM execution, full WAF/CRS or live bundle updates are implied.

## Layout

- Root `bundle`: strict decoding and exact resource references; no app knowledge.
- Root `match`: reusable fact extraction/selection primitive.
- `apps/marker`: independent Match → literal marker app; no WAF/Coraza/SDK dependency.
- Root `match/config`: shared typed header-resource preparation.
- `apps/waf`: app compiler and execution, including typed Match output and action mappings.
- `apps/waf/inspect`: policy preparation and Coraza inspection; no bundle/Match/Envoy imports.
- `apps/waf/envoy/filter.go`: Envoy callbacks; adapts owned inputs and applies app outcomes.
- `hosts/envoy/internal/filter/match.go`: standalone body Match adapter.
- `hosts/envoy/integration/bootstrap`: embeds a supplied bundle into the bootstrap template, without compiling it.
- `hosts/envoy/integration/envoy.json`: template with separate WAF and Marker bundle placeholders.
- `hosts/envoy/integration/native_test.go`: real traffic and native invalid-configuration assertions.

The native harness renders the template with local module paths, dynamically chosen
loopback ports, and an in-process Go backend. There is no separate backend service
or container lifecycle to manage.

## Qualification and limits

The current direct macOS arm64 run passed against Envoy
`0a804c57cf5fbd56da553062bebc9b88482b0d1b/1.40.0-dev/Clean/RELEASE/BoringSSL`:

- Bundle-fed policy selection, enforcing and detection-only inspection, no backend
  receipt for blocked requests, and 40 concurrent body selections.
- Native Envoy config rejection for an unresolved policy reference.
- Compiler rejection of wrong versions/output types/phases, unavailable facts, raw
  SecLang, bad placement/handoffs, fail-open mappings, duplicate resources and unknown fields.
- Configured rule-value/ID changes take effect; prepared state owns its input.
- Existing body Match invalid-input, limit, encoding and trailer cases.

The SDK workaround remains `GODEBUG=cgocheck=0`, explicitly set by the test/fixture.
It disables the runtime pointer checker and is not production ownership qualification.
Root primitive packages do not require that setting.

This slice does not implement generic workflow execution, body/response WAF, HTTP/2
qualification, performance budgets, configuration streaming, revocation, EG/k3d,
remote mTLS or SDS. A complete bundle is prepared atomically for one WAF app factory;
there is no claim of transactional activation across independent Envoy filters.

## Marker and placement

The fixture also consumes `examples/config/marker.json`: `/chat` selects
`chat-request`, `/observe` selects `observe-request`, and other paths skip marking.
Tests assert upstream marker values, spoof removal, independent typed output and
that a WAF block prevents Marker execution. Hostname-specific activation remains
[design work](ACTIVATION.md); all hosts currently share this fixed chain.

## Shared JSON body parser

The standalone body-Match stage now parses once through `body.Parse`, with a 4096-byte,
32-depth, 256-node profile. Both `/model` (string) and `/stream` (boolean, if present)
are validated from that document. Invalid stream types reject even if model matches.
Original request bytes are preserved. Depth/node/byte limits return 413; invalid JSON,
Unicode or fact types return 400. The diagnostic upstream `x-fig-body-parses` reports
one on successful body evaluation and is stripped from incoming requests.

Native tests cover two-fact sharing, Unicode/node rejection, no backend receipt on
errors and unchanged forwarded bytes. The bundle-level Body resource and cross-app
sharing described in [the contract](primitives/BODY.md) remain unimplemented.

## Code ownership

Run `make test` or `make native-test` from the repository root. The local Envoy build
is owned by `hosts/envoy/Makefile`. App callbacks live in separate `apps/marker/envoy`
and `apps/waf/envoy` modules; `hosts/envoy/cmd/fig-envoy` composes their factories.
`hosts/envoy/internal/filter` contains the standalone Match adapter used by this fixture.
See the [root README](../README.md) for module boundaries. Current qualification is
local native Envoy only; APIs remain experimental.

## WAF → Marker request handoff

The same `make native-test ENVOY_BIN=/path/to/envoy` command also runs
`TestNativeHandoff`. Each case starts one local Envoy, using the WAF bundle and
`examples/config/marker-inspection.json` plus explicit producer/consumer bindings.
A clean `/observe` request yields `waf-clean`; detection yields `waf-detected`.
An enforcing block prevents Marker and backend execution. The value comes from
filter state, with client diagnostic headers stripped.

Other cases cover two producer instances, duplicate publication, missing required
and optional input, stale generation, wrong type and missing projection fields.
Failure cases assert HTTP 500 and no backend receipt; 40 concurrent requests check
isolation. Original independent Marker tests still run. The fixture renders bindings
in template order and checks declared dependencies before starting Envoy; fault
injection cases deliberately alter configuration after that admission check.

The [request-data contract](REQUEST_DATA.md) records the exact schema and remaining
qualification gates. Only compact header-time scalar results cross filter instances;
the parsed JSON body is still owned by its original filter.
