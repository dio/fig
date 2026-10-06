# Fig rationale

Status: initial proposal, 2026-10-06. [DESIGN.md](DESIGN.md) states the proposed
concepts and invariants. This document explains the decisions and alternatives; it
does not claim that Fig implements them.

## Why start with concepts

The immediate question was whether Plum's component views could receive independently
streamed configuration. That exposed an earlier question: what constitutes a view when
the consumers include routing, inspection, MCP profiles/tools, typed decisions, caches
and other independently configured behavior?

Choosing resource type URLs before answering that would encode today's application
shapes into a transport contract. We first need stable meanings for facts, selection,
prepared state, activation and request lifetime.

## Evidence from the existing projects

The comparison used local source on 2026-10-06. Plum and Citrus had unrelated local
changes, which were left untouched. Revisions below identify the checked-out bases;
this was source inspection, not a fresh execution qualification of those repositories.

| Project | Inspected base | Relevant finding |
|---|---|---|
| [Plum](https://github.com/tetrateio/plum) | `d5f516e` | `pipeline/match/match.go` maps a body model to a provider binding; `pipeline/distributor/distributor.go` publishes Pick, Match and Adapt sequentially |
| [APIx](https://github.com/tetrateio/apix) | `197a54e` | `serve/evaluate.go` prepares indexed assignments; `serve/body.go` completes body routing; `serve/continuation.go` captures generations and checks authority at dispatch |
| [Citrus](https://github.com/tetrateio/citrus) | `d6966f9` | WAF policy content and policy-bound runtime mode are separate; application access and WAF have different lifetime/outage rules |
| [Kona](https://github.com/dio/kona) | `3a0eff9` | Separate HTTPS data delivery over Envoy-owned mTLS, with a Helm deployment and qualified application CA rollover |

Plum's current view is application-specific, not a universal matcher. Its current
body resolution reads the active view; atomic request-wide generation capture must
be introduced deliberately rather than assumed. The inspected checkout did not have
the older Target/Split/Chain routing implementation returned by stale graph entries.
Those concepts in Fig are proposed semantics, not claims about current Plum code.

Citrus also has a wiring discrepancy worth preserving in our reasoning: its WAF runtime
code and module README describe policy-bound dynamic mode capture, while the inspected
`scripts/generate-module-overlay.py` explicitly excludes runtime activation transport
from the native overlay. This is useful contract evidence, not proof that the current
native module exercises that dynamic path. APIx's final dispatch checks are a stronger
reference for revocable access than a generic snapshot pointer alone.

## Decision 1: serialize extraction and selection

**Choice:** declare both extraction and selection in versioned data; compile before
publication. The runtime hosts a bounded registry of implementations.

This makes behavior reviewable and transportable across embedded, file and remote
sources. It also allows validation of phase dependencies and resource bounds before
traffic reaches the configuration.

**Alternatives:** application callbacks are convenient but hide behavior from the spec;
a general scripting language provides flexibility but enlarges the safety, performance
and compatibility contract. Begin with typed extractors and predicates. Add expressiveness
when a demonstrated use case cannot be represented clearly.

Serializable does not mean entirely generic: an LLM extractor, HTTP selector and WAF
policy can each have their own schema. JSON syntax alone does not establish a contract.

## Decision 2: separate selection from execution

**Choice:** Match produces typed context or a routing-plan reference. Policy stages
make their own decisions. The executor traverses Target/Split/Chain; Pick finds a host;
Adapt builds a target-specific request.

A selected route is not an authorization permit, and a selected WAF policy is not a
successful inspection. Combining all of these into one "match result" would obscure
which stage can deny, wait, retry or retain state.

APIx currently combines several operations in its evaluator. We borrow its generation
and continuation guarantees without requiring its exact implementation decomposition.
WAF stays an inspection stage that can span request and response phases, rather than
being reduced to a routing predicate or fallback destination.

## Decision 3: share the view lifecycle, preserve application semantics

**Choice:** share preparation/publication/capture/release concepts. Keep payload types,
validation and safety policies with their consumers.

An indexed route table and a compiled WAF engine are both prepared configuration, but
have different costs and request state. A universal `map[string]any` view would defer
type errors and lose the relationships the compiler needs to validate.

Similarly, a Go `View[T]` container alone would not solve phase availability, authority,
activation or native resource lifetime. Its eventual API should follow those contracts.

## Decision 4: capture configuration, revalidate authority

**Choice:** capture a coherent generation before relevant waits, while checking current
permission to perform a protected action at dispatch.

APIx demonstrates why both are necessary. A request should not parse its body under
one generation and route using another, but a captured authorization result cannot
outlive its permitted deadline, revocation or invalidating replacement.

For WAF, retaining an engine for an admitted transaction preserves consistent request
and response inspection. For API access, replacement may fence a continuation before
forwarding. Neither behavior should become a universal rule for all resources.

## Decision 5: make retry explicit and bounded

**Choice:** sequential retry and fallback belong to one executor with global budgets.
Retry remains on a logical target; fallback advances a chain. Split selection remains
fixed during retries.

This avoids accidental multiplication of attempts by nested policies or another host
retry loop. It also makes replay risk visible: an LLM call may be billable, and an API
mutation may have completed even when no response was received.

We reject automatic replay inferred solely from a transport error, unlimited buffering,
resampling a split on every failure, and fallback that bypasses policy denial. Hedging
would require concurrent attempt ownership, duplicate-effect rules and cancellation
qualification; it is intentionally deferred.

## Decision 6: separate delivery from coherent activation

**Choice:** transport resources independently where useful; activate coupled resources
as one validated generation.

One large snapshot simplifies consistency but forces unrelated updates to travel and
compile together. Independent immediate publication reduces update size but can expose
broken references between a matcher, target catalog, adaptation and WAF mode/policy.
A generation boundary provides coherence without requiring a global snapshot for every
update. It introduces staged-version retention and limits, which must be explicit.

A possible pipeline revision resource names exact dependencies. It remains a proposal;
we must first decide the scope of atomicity for the concrete consumers.

## Decision 7: keep the data channel independent of EG topology

**Choice:** use a small bootstrap reference to an application configuration source.
Retain EG's role in topology, filter installation and transport TLS where supported.

Kona demonstrates the HTTPS polling form and separate application certificates. It
allows application updates without requiring a listener or route update each time.
File and embedded sources should reach the same semantic preparation boundary.

Typed Delta ADS is a candidate because its protocol already describes named resources,
versions, removals and acknowledgments. A smaller streaming protocol might reduce
integration cost. We have not selected either. Reusing xDS messages does not supply
cross-resource atomic activation or make EG a custom-resource distribution server.
See the [xDS protocol](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol.html)
for the protocol's own semantics; Fig must document any additional activation contract.

## Decision 8: share certificate management without sharing trust by default

**Choice:** EG's control-plane channel and the application data channel use separate
CA/leaf identities, even if one cert-manager installation manages both.

This keeps application signing authority and rollover independent of control-plane
trust. The same Envoy process can use different credentials for its different peers.
SDS delivers credentials; it does not mint them. A certificate-manager controller with
access to both sets of Secrets remains shared privileged infrastructure.

A shared CA is a possible explicit deployment choice, with coupled trust consequences.
No deployment topology can replace resource-level authorization by scope/type/name.
See [Kona's certificate rationale](https://github.com/dio/kona/blob/main/docs/certificates.md)
and [EG's cert-manager installation path](https://github.com/dio/kona/blob/main/docs/eg-cert-manager.md).

## Decision 9: consumers are extensible, and completion can be local

**Choice:** treat WAF, API access, LLM, MCP profiles/tools, Jev, cache and guardrails as examples
of typed components. Their selection outputs and prepared contents stay specific to
their contracts. Shared composition describes phases, dependencies, outcomes and
lifetime without a closed application-name list.

MCP introduces profile/catalog consistency, tool identity, sessions and side effects.
Jev introduces typed decision evaluation that need not call an LLM. Cache introduces
mutable entry/fill state and successful local completion without upstream selection.
Together they expose assumptions that a routing-only example would leave hidden.

We reject forcing every result into a provider/backend tuple, representing every
component as a fallback target, or putting mutable cache entries into an immutable
configuration view. We also avoid assuming that no upstream call means no authorization
check. Required access and inspection gates still apply before a cached result can be
served.

This extends the conceptual scope; it does not establish implementation support or
claim these projects already share one runtime. Component registration and schema
validation must reject unsupported behavior explicitly.

## Decision 10: bring-your-own guardrails through explicit bindings

**Choice:** model guardrails as typed stages with approved execution bindings, policies
and attachments. A remote adapter lets a team operate its own implementation while the
host retains responsibility for selection, enforcement, bounds and observations.

The user-confirmed reference is **LiteLLM custom guardrails**. Its documentation
was checked on 2026-10-06; the related team BYO flow is an additional management example:

- [Team BYO guardrails](https://docs.litellm.ai/docs/proxy/guardrails/team_based_guardrails)
  separates scoped registration from review and activation; this registration path is
  restricted to its Generic Guardrail API.
- [Generic Guardrail API](https://docs.litellm.ai/docs/adding_provider/generic_guardrail_api)
  describes an HTTP contract for extracted content and decisions to block, allow
  unchanged, or intervene with modifications. It is documented as beta.
- [Custom guardrails](https://docs.litellm.ai/docs/proxy/guardrails/custom_guardrail)
  describes code implementing `apply_guardrail` and lifecycle hooks for input/output
  processing. This is a different integration path from team HTTP registration.

For the custom-class path, the mapping is: content extraction and `input_type` become
an explicit input/phase contract; returned inputs become an allow or validated
transformation; intentional rejection becomes block. An adapter must distinguish a
policy rejection from an unexpected exception instead of translating every exception
into either an allow or a policy verdict. Python lifecycle hook names do not determine
Fig's release guarantees automatically.

Existing Python classes could be hosted behind a versioned adapter service. A native
registered implementation is another option, with separate qualification. Both require
capability checks for supported content, mutation and streaming behavior. The initial
proposal does not embed a Python interpreter or promise to load every LiteLLM plugin.

Fig borrows the separation of user-owned implementation, declarative attachment and
host-enforced result. It does not adopt LiteLLM's complete management model, execute
arbitrary Python from a streamed document, or claim compatibility before an adapter
has been versioned and tested. Its local implementation registry and remote binding
model are independent deployment choices.

The following are Fig proposals, not assertions about LiteLLM behavior:

- Required checks are governed by publication authority, not optional caller input.
- Error and policy denial have different meanings; failure behavior is explicit.
- Content transformations preserve provenance and trigger revalidation of dependent
  facts, routing, authorization and cache keys.
- A result released from cache is still subject to required policy. Fallback does not
  erase an input or tool-call denial.
- Concurrent inspection and observation after release cannot claim to have prevented
  upstream processing or content delivery. Streaming needs an explicit buffering and
  release contract.
- Credentials, outbound endpoint authorization and allowed data sharing are part of
  binding admission; mTLS alone does not authorize transmitting arbitrary request data.

This case broadens the view abstraction usefully: a prepared view may select a remote
policy execution binding rather than contain the policy engine itself. Mutable calls,
stream holdback and transformed content still belong to request state. The common
contract is preparation, typed invocation and lifetime, not a universal guardrail model.

## Decision 11: compose modules by placement, not whole apps in one chain

**Choice:** an app contributes individually placeable modules. Each module declares
where it can run and which typed facts, handoffs and capabilities it requires. Users
compose instances subject to those contracts.

LLM is the motivating example: downstream Match selects the logical operation and
plan, while upstream Adapt and credential injection operate on the actual attempt's
target. Pick occupies a host-selection integration point. WAF is downstream-only,
including its response inspection callbacks. These placements cannot be represented
accurately by ordering a single "LLM app" beside a single "WAF app."

A fixed universal filter list would prevent valid compositions. Unchecked arbitrary
ordering would permit missing facts, invalid signatures, leaked credentials or bypassed
protection. Declared placements, phase-aware handoffs and mandatory ordering constraints
allow flexibility while making invalid arrangements fail before activation.

Upstream placement also exposes a lifetime distinction: retry/fallback can change the
target, adaptation and credential for each attempt while downstream policy state remains
attached to the client stream. Treating both as the same request-global mutable object
would risk carrying provider-specific state into another attempt.

Logical placement is a Fig contract. Physical Envoy listeners, HTTP filter chains,
clusters and callback APIs are host-adapter concerns. We must prove lowering and actual
traffic behavior for each supported arrangement. Having an installation API or a module
binary alone is not evidence that an arrangement executes correctly.

## Decisions still required before implementation

1. **Examples and phases:** exact fact sets, selection results and event traces for
   LLM routing, API access, WAF, MCP profiles/tools, Jev, cache and guardrails; where trusted
   identity becomes available, where sessions bind, and where local completion is legal.
2. **Placement and handoffs:** module descriptor capabilities, compatible locations,
   typed producers/consumers, request/response traversal, per-attempt lifetime and actual
   host lowering. Distinguish downstream-only WAF from upstream adaptation/authentication.
3. **Schema:** protobuf, JSON Schema, or another authoritative definition; error
   vocabulary; compatibility and extension registration.
4. **Activation scope:** which resources can update independently and which must move
   together; staging bounds and recovery from missing versions.
5. **Authority contracts:** expiry, invalidating replacement, removal, rollback and
   outage behavior per resource type.
6. **Execution:** precise failure classification, host reselection, replay permission,
   quota accounting, timeout inheritance and response commitment.
7. **Native lifetime:** ownership of engines, request transactions, credentials and
   host handles; limits for long-lived requests and retained generations.
8. **Delivery:** Delta ADS or another protocol, reconnect behavior, slow subscribers,
   authorization, message limits and how streaming fits the host integration.
9. **Component state:** MCP session/catalog consistency, tool replay permission, Jev
   result validation, cache isolation/invalidation and fill ownership, and guardrail
   transformation/streaming contracts. Determine which
   guarantees belong to shared composition versus the component implementation.
10. **Qualification:** focused model tests followed by real-host traffic, including
   backend non-receipt on denials and cancellation. Existing project evidence does
   not automatically qualify a new Fig adapter.

These should be resolved with the smallest executable vertical slice after agreement
on the concepts. No new distribution service, public CRD or generic plugin framework
is required to settle the first set of semantics.
