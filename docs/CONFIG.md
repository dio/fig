# Configuration and encapsulation

Status: experimental v1alpha1, consumed by the native WAF app. The
[example bundle](../examples/config/waf.json) is embedded into the Envoy filter config;
the module decodes, validates and prepares it during configuration creation. An invalid
bundle prevents that filter configuration from loading. There is no live-update manager yet.

## Primitive versus app

**Match is a primitive. WAF is an app.** Apps compose the primitives they need and own
their domain contracts. There is no WAF knowledge in the Match primitive, and no
application registry switch inside the bundle decoder.

| Layer | Owns | Must not own |
|---|---|---|
| `bundle` | Bounded strict decoding, scope/generation envelope, exact references, resource ownership | Match semantics, Coraza, Envoy callbacks |
| `match` | Extractors, typed predicates, ordered selection, prepared views and evaluation | WAF policy fields, bundle transport, HTTP replies |
| `spike/envoy/apps/waf` | WAF schema validation, Match output contract, policy resolution, supported composition, outcome mappings | Generic publication infrastructure, Envoy handles |
| `apps/waf/inspect` | Header-rule validation, Coraza engine preparation and transaction lifecycle | Bundle references, Match, Envoy, HTTP response mapping |
| `spike/envoy/module/wafapp.go` | Envoy inputs, per-stream result, metadata and local replies | Rule compilation, policy selection algorithms, generic bundle validation |
| `bootstrap` | Embed supplied JSON into an Envoy fixture | Validate application semantics or prepare engines |

The WAF app uses Match to select a policy reference and resolves it to a prepared
inspection engine. That resolution is **not upstream Pick**. Pick will resolve a
logical destination to a host for an app that dispatches upstream. Apps need not use
every primitive. Future LLM/MCP apps may compose Match, Pick and Adapt without putting
their fields into either Match or WAF.

The app currently lives in the spike's nested Go module to keep Coraza and native
SDK dependencies out of Fig's core module. The app packages themselves import no
Envoy SDK. This is deliberate experimental placement, not a stable public app API.

## Authoring and envelope

JSON-compatible data is the internal contract; YAML and existing Gateway API or
AI-oriented APIs can be authoring adapters. This does not require a new public CRD.
Bootstrap transport addresses, TLS material and file watching remain separate.

A bundle has `apiVersion`, `scope`, `generation`, and `resources`. Each resource has
`type`, `name`, `version`, and `spec`. Type includes its schema version. Resource
versions and generation identifiers are opaque equality tokens.

References are `{type, name, version}` within the bundle scope. Cross-scope lookup is
not supported. A second version of the same type/name in one bundle is rejected.
Every reference, including defaults, must resolve exactly; nothing resolves to an
implicit latest version at request time.

The Envoy bootstrap selects an entry and embeds the bundle:

```json
{
  "entry": {"type": "fig.pipeline/v1alpha1", "name": "edge", "version": "1"},
  "bundle": {"apiVersion": "fig/v1alpha1", "scope": "demo", "generation": "1", "resources": []}
}
```

The empty resource list above only illustrates the wrapper; it is invalid as a runnable
bundle. The complete working example is linked above. One fully prepared object owns
the matcher, prepared policies and action mappings. Factories publish no partial state.
Each stream retains its factory's object. Replacing a generation dynamically is future
work; the spike does not claim multi-config atomic updates across independent filters.

## Match resource

The app lowers its Match resource to the existing primitive. The resource declares
`phase`, `outputType`, `facts`, `rules`, and an explicit `onNoMatch`:

```json
{
  "phase": "request-headers",
  "outputType": "fig.waf-policy-ref/v1alpha1",
  "facts": [{"name": "path", "extractor": "input-field/v1", "type": "string", "args": {"name": "path"}}],
  "rules": [],
  "onNoMatch": {"result": {"policyRef": {"type": "fig.waf-policy/v1alpha1", "name": "baseline", "version": "1"}}}
}
```

`onNoMatch` is either `{"return":"no-match"}` or a typed default result. Invalid and
pending facts never become a default. The WAF app registers the meaning of
`fig.waf-policy-ref/v1alpha1` in its compiler, checks all references, and prepares
`match.Prepared[selection]`; the generic primitive does not know that selection type.

For this app slice only header-phase string fields `path`, `method`, and `authority`
are available. They are request claims, not authenticated identity. Unknown fields,
extractors or output types fail compilation. Other apps can use the primitive's body
extractor with their own lifecycle contracts; this restriction is WAF-app-specific.

## WAF policy resource

```json
{
  "engine": "coraza",
  "mode": "enforce",
  "inspection": "request-headers",
  "rules": {
    "format": "header-rules/v1",
    "items": [{"id": 1001, "header": "X-Fig-Attack", "equals": "attack"}]
  }
}
```

`mode` is `enforce` or `detect`; both inspect. `header-rules/v1` supports 1–128 rules
with unique positive IDs, ASCII alphanumeric/hyphen header names of at most 128
characters, and equality literals of 1–128 ASCII letters, digits, dots, underscores
or hyphens. The inspection package generates phase-1 SecLang internally. Input cannot
inject engine-mode directives, includes, arbitrary actions or additional phases.

This **replaces the previous unimplemented raw SecLang draft**. The narrow format
makes the implemented capability explicit. Full SecLang/CRS should be a separately
versioned, validated preparation adapter with captured file dependencies, not a
permissive string accepted by this compiler. Body/response WAF remains unimplemented.

## App composition resource

`fig.pipeline/v1alpha1` names its owning `app: fig.waf/v1alpha1`, placement and steps.
The current app supports exactly:

1. `fig.match/v1alpha1`, with a Match config reference.
2. `fig.waf.inspect/v1alpha1`, consuming that step's WAF-policy-reference output.

Other shapes, additional pipelines, duplicate step IDs, wrong placement, missing
handoffs, forward references and unsupported module types fail explicitly. This is
an app-specific compiler, not an implemented generic workflow engine.

Step outcomes are distinct:

- Match `onNoMatch`: terminal HTTP status 400–599.
- Match `onError`: terminal HTTP status 400–599.
- WAF `onBlock`: terminal HTTP status 400–499.
- WAF `onError`: terminal HTTP status 500–599.
- Successful inspection: continue, by the module contract.

The first slice supports `localReply` status mapping only, not custom response bodies,
retries or jumps. A required WAF cannot continue on error or denial. Detection-only
is a policy mode, not a fail-open error handler. Runtime results carry policy, mode,
matched state, rule ID and next action separately from selection results.

## Validation and preparation

The [structural schema](../schemas/fig-bundle.schema.json) is useful for tooling, but
runtime correctness does not depend on a JSON Schema validator. Runtime preparation:

1. Limits config bytes to 1 MiB and JSON depth to 64; rejects duplicate keys, trailing
   data, unknown typed fields, null roots and duplicate resource identities.
2. Validates the chosen app, pipeline shape, placement and response mappings.
3. Prepares every policy using the WAF-owned schema and rule constraints.
4. Prepares every Match resource, checks the host fact contract and resolves every
   policy reference, including unused rules/defaults, to an exact prepared resource.
5. Returns the executable app only when every step succeeded.

Unrecognized resource types are rejected by this app compiler. The envelope decoder
accepts arbitrary resource types; future apps provide their own compiler/registry.
The decoder returns owned copies, and prepared components retain no mutable caller
configuration. The current engines have no app-owned external resources; future
engines/callouts must add explicit prepare-failure cleanup and retained-view leases.

## Running and evidence

```sh
make native-test ENVOY_BIN=/path/to/matching/envoy
```

The test reads the canonical example bundle and embeds it into `fig-waf-app`'s config.
Envoy calls the app compiler at configuration creation. Live requests then exercise
policy selection, inspection, detection-only behavior and local reply mappings.
The legacy body-dependent Match filter remains a separate fixture stage; it has not
been migrated into this header-only WAF app's bundle and is not claimed to share its
activation boundary.

Negative tests cover wrong versions, output types, late facts, unavailable host facts,
raw SecLang, wrong placement, missing handoffs, fail-open status mappings, duplicates
and unknown fields. A native validation test also checks that Envoy rejects an
unresolved policy reference. A changed-rule test proves rule ID/value are consumed
from the bundle rather than supplied by a hardcoded smoke rule.

Next boundaries to design: broader app/module descriptors, response representations,
body buffering contracts, activation/revocation, and delivery adapters. Keep those
changes outside the Match primitive and inside the layer that owns their semantics.
