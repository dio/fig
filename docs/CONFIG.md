# Configuration format: first draft

Status: v1alpha1 design draft. The native Match/WAF spike exercises the behavior;
it does **not** load this bundle format yet. [The example](../examples/config/waf.json)
and [structural schema](../schemas/fig-bundle.schema.json) are review artifacts.

## What belongs in configuration

Use JSON-compatible data as the internal contract. YAML may be an authoring syntax.
Gateway API, AI Gateway APIs and existing policy APIs remain possible user-facing
surfaces; their adapters compile to this contract. Fig does not require users to
manage another public CRD.

Separate four concerns:

| Concern | Example | Owner |
|---|---|---|
| Fact extraction and selection | Request path -> WAF policy reference | Match |
| Prepared application policy | Coraza rules, inspection scope, enforcement mode | WAF |
| Placement and dependency wiring | WAF consumes early Match output | Composition |
| Outcome handling | Continue, local block, fail closed on execution error | Host adapter constrained by module contract |

Delivery addresses, TLS material and watched files belong to source/bootstrap config,
not a rule or Match result. Initial bundles contain resolved configuration bytes; a
later resource channel may deliver individual resources independently.

## Envelope and identity

The draft bundle carries `apiVersion`, `scope`, `generation`, and `resources`.
Each resource has `type`, `name`, `version`, and `spec`. Type includes its schema
version. Resource versions and generation identifiers are opaque strings, compared
for equality rather than numeric ordering.

A reference is `{type, name, version}` within the enclosing scope. Cross-scope
references are excluded initially. References identify exact dependencies; names
alone do not silently resolve to whatever version is newest at request time.

A bundle is the first atomic preparation unit: validate all resources, resolve every
reference, prepare every dependency, and only then activate one generation. Failed
preparation must not partially publish. This is an initial transport adapter, not a
requirement that future delivery be state-of-the-world. Future deltas stage resources
and activate an explicitly complete dependency set.

## Match resource

The first spec reuses the Go spike's explicit phases, typed values and predicate
operators, and adds an output type plus typed dependency references:

```json
{
  "phase": "request-headers",
  "outputType": "fig.waf-policy-ref/v1alpha1",
  "facts": [{
    "name": "path",
    "extractor": "input-field/v1",
    "type": "string",
    "args": {"name": "path"}
  }],
  "rules": [{
    "id": "observe",
    "when": {"op": "equals", "fact": "path", "values": [{"kind": "string", "text": "/observe"}]},
    "result": {"policyRef": {"type": "fig.waf-policy/v1alpha1", "name": "observe", "version": "1"}}
  }],
  "onNoMatch": {
    "result": {"policyRef": {"type": "fig.waf-policy/v1alpha1", "name": "baseline", "version": "1"}}
  }
}
```

`onNoMatch` is required: either `{"return":"no-match"}` or a typed result. This
replaces the spike's implicit nil-default convention at the wire boundary. Pending
input is never no-match. Invalid input is a failed evaluation, not a default result.

The installed output schema validates all rule/default values and declares which
fields are references. `outputType` is open to installed consumers: a routing plan,
MCP binding or decision definition does not become a WAF-shaped result.
`input-field/v1` remains an adapter seam. The compiler must know that the host actually
provides the requested field and its trust/phase/representation; a string in `args`
does not establish trusted identity. Richer representation and extraction contracts
remain in the [Match design](primitives/MATCH.md).

## WAF policy resource

A WAF policy declares:

- `engine`: initially `coraza`.
- `mode`: `enforce` or `detect`; detection still runs inspection.
- `inspection`: initially `request-headers` only.
- `rules`: a SecLang-format inline rule document in this draft.

The engine/mode/scope fields are authoritative. A future compiler must reject
conflicting SecRuleEngine directives, unsupported phases and unavailable capabilities.
Inline rules do not imply filesystem Include access: capture and authorize dependencies
before preparation. This draft does not promise arbitrary SecLang support or CRS.

The current executable spike has a fixed phase-1 rule matching header
`X-Fig-Attack: attack`; its policy configuration varies mode only. Accepting configurable
rules is a subsequent implementation step, not something this document claims is wired.
Body/response inspection requires phase capabilities, buffering limits and retained
transactions, so it must not be enabled merely by changing the `inspection` string.

Mode is colocated with policy for the first slice so the engine and mode prepare
atomically. An independently streamed mode resource can be added later, with an exact
policy revision reference as required by the broader design.

## Pipeline attachment

A pipeline declares placement and an ordered list of named steps. Each step references
an installed module and supplies either a config reference or a typed input from a
previous step. In the example, `select-policy` produces a WAF-policy selection and
`inspect` consumes it.

`onNoMatch`/`onError` on a step describe host behavior for unavailable results. WAF
`onBlock` describes the response mapping for an actual policy denial. They are not
interchangeable. A no-match is not an allow; an engine error is not a policy block.

The first actions are bounded and local:

- `continue`: proceed in this chain after a successful component result.
- `localReply`: terminate processing with the declared HTTP status.

Required WAF inspection uses `onError: localReply(503)` and
`onBlock: localReply(403)`. It cannot configure `onBlock: continue`; detection-only
behavior is explicit in the policy instead. No arbitrary `goto`, retries or fallback
on denial. A terminal action ends the protected path.

The example has no separately configurable `onAllow`: successful inspection continues
by the module contract. Avoid configuring choices that have only one valid behavior.
Protocol-specific response mapping belongs to this downstream HTTP attachment, not
the host-independent Match core.

## Runtime outcome is a different object

An inspection produces a request-owned result such as:

```json
{
  "policy": "baseline",
  "mode": "enforce",
  "matched": true,
  "action": "block",
  "ruleId": 1001
}
```

The same rule under `mode: detect` produces `matched: true, action: continue`.
No match produces `matched: false, action: continue`. Execution failure produces
`action: error`, mapped by attachment to a terminal response. This is bounded next-action
selection, not plan execution. Credentials and raw matched content are omitted.

## Validation layers

The JSON Schema checks the envelope and coarse resource shapes. It intentionally
leaves registered extractor arguments and consumer outputs open. It is not the whole
compiler, and successful JSON Schema validation is not activation readiness.

Semantic preparation must additionally reject:

1. Duplicate resource identities or step/rule/fact IDs.
2. Missing, wrong-type, out-of-scope or wrong-version references, including defaults.
3. Unknown extractors/modules/output schemas and incompatible typed predicates.
4. Cycles, forward or missing handoffs, incompatible placements or representations.
5. WAF selection unavailable before required inspection, unsupported inspection phases,
   or an attachment that bypasses required protection on error or block.
6. Unsupported rules, conflicting engine settings, uncaptured file access and exceeded
   configuration/body/compilation bounds.

The spike currently validates individual configs and catches an unknown policy at
request time. The bundle compiler should move that error to preparation; the runtime
failure remains defense against incomplete or unavailable state.

## Implementation order

1. Review these names and boundaries using the working header WAF and body Match cases.
2. Implement strict bounded bundle decoding and semantic dependency checking, initially
   for these three resource types. Unknown keys and duplicate JSON keys must fail.
3. Lower a validated bundle into the existing module factories; remove handwritten
   policy selection/handoff naming from the fixture.
4. Prove invalid bundles do not activate and one request cannot mix revisions.
5. Add delivery adapters after the preparation/activation contract is exercised.

Open decisions: resource naming conventions, schema registry/discovery, representation
references, separate mode resources, limits inherited from host policy, and routing-plan
payloads. Do not freeze a universal component outcome or wire protocol from one WAF test.
