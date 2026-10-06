# Fig design

Status: initial proposal, 2026-10-06. This repository contains concepts, not an
implemented runtime, stable schema, or interoperability claim. YAML below illustrates
semantics; it is not a parser contract. [RATIONALE.md](RATIONALE.md) records why these
boundaries exist and which decisions remain open.

## 1. Developer objective

Describe how a runtime extracts facts, selects component behavior, and executes
that behavior using serializable specifications. Consumers are open-ended: MCP
profile routing, MCP tool routing, Jev decisions, caching, bring-your-own guardrails,
LLM routing, API gateway
behavior and WAF inspection are examples, not a closed set of applications.

A selected behavior may inspect, evaluate, transform, serve a local result, invoke a
tool, or dispatch upstream. Neither an HTTP destination nor a model/provider pair
is a required universal output. The initial host-oriented examples use request
processing; other event models require explicit phase and lifecycle adapters.

Operators must be able to deliver configuration independently of Envoy topology,
observe which configuration is accepted and active, update it safely, and explicitly
retire it. Request execution must remain understandable when configuration changes,
authority expires, or a backend fails.

## 2. Ownership

| Component | Owns |
|---|---|
| Authoring adapter | Translate a user-facing API into validated Fig specifications |
| Compiler | Type checking, dependency checking, indexes and executable preparation |
| View runtime | Publication, capture, retirement and lifetime of prepared views |
| Match | Extract declared facts and select application context or a routing plan |
| Component stage | Typed behavior such as inspection, decision evaluation, caching, authentication or quota |
| Executor | Routing/invocation-plan traversal, attempt budgets, retry, fallback and cancellation |
| Pick | Resolve a logical target to a compatible concrete host |
| Adapt | Construct the target-specific request and transform its response |
| Distribution adapter | Authenticate delivery and transport versioned resources |
| Host adapter | Bind execution to Envoy or another host's events and resource handles |

Management owns desired state and authorization to publish. Delivery does not create
that authority. A selected route or accepted configuration does not itself authorize
a protected operation, including serving a local cached result. Private credential custody stays outside ordinary routing specs.

Gateway API, AI-oriented APIs, files and other authoring surfaces can be adapters.
Fig does not require a new public CRD. An internal serializable execution contract
still needs precise semantics even when the user-facing API is an existing standard.

## 3. Specifications, views and request state

These are three different objects:

- **Specification:** serializable, versioned declarative data describing behavior.
- **Prepared view:** a validated runtime projection, including compiled matchers,
  indexes or engine references. Published contents are immutable.
- **Request state:** facts, a captured configuration generation, selection results,
  policy transactions, attempt history and cancellation/deadline state.

```text
embedded / file / remote resource
             |
       decode and validate
             |
       compile and prepare
             |
       publish a generation
             |
       capture for a request
             |
       execute and release
```

Compilation must not change active state. Failed preparation preserves the previous
active generation, subject to its existing expiry or retirement rules. Successful
publication makes a complete prepared generation available to new captures.
Prepared engine objects may have internal synchronization; their configuration is
immutable, and mutable inspection state belongs to individual request transactions.

Resource identity is `(scope, type, name)`. Schema version identifies how to interpret
payloads. Resource version identifies content or a revision, with an equality contract;
it must not silently imply numeric ordering. Generation identity identifies a coherent
set of prepared resources. Delivery response identifiers are a separate concept.

## 4. Serializable fact extraction

Each fact declaration specifies:

1. Source and earliest availability phase.
2. A registered, versioned extractor and its serializable arguments.
3. Result type and explicit normalization.
4. Input bounds, parsing limits and any buffering requirement.
5. Missing, malformed and unsupported-input behavior.
6. Sensitivity and permitted use in observations.

Illustrative declaration:

```yaml
facts:
  model:
    source: request.body.json
    extractor: json-pointer/v1
    pointer: /model
    valueType: string
    availableAt: request-body-complete
    maxBodyBytes: 65536
    onMissing: reject
    onInvalid: reject
```

Fact states include **pending**, **present**, **missing**, and **invalid**. Pending
means its phase has not completed; it must not accidentally take the missing/default
branch. A declaration can apply an explicit error policy after extraction completes.

Header multiplicity, case handling, URL/path normalization, JSON duplicate keys,
content type, compression, empty values and coercions require defined extractor
semantics. An extractor must not perform implicit network IO or execute arbitrary
code supplied in a document. Sensitive extracted values are not logged by default.

The initial extractor set should be small: method, authority/host, path, a named
header, and a bounded JSON pointer. Query extraction can follow with explicit decoding
and repeated-key rules. Selection randomness is supplied by the executor, not hidden
inside an ordinary fact extractor.

## 5. Serializable selection

A selection specification declares predicates over typed facts and produces typed
values or references. It also defines precedence, tie behavior and no-match behavior.

```yaml
selection:
  strategy: first-match
  rules:
    - when:
        equals: {fact: model, value: support-chat}
      result:
        routingPlanRef: support
  onNoMatch: reject
```

Start with explicit ordered first-match rules and basic typed comparisons/boolean
composition. Do not introduce a general expression language until concrete use cases
require it. A compiler rejects unknown fact references and incompatible comparisons.

The selection result can identify a deployment, route/operation, MCP profile or tool
binding, Jev decision configuration, cache policy, destination binding, policy
references, or component-owned typed attributes. LLM model/provider details
belong to an LLM-specific binding or result, rather than mandatory universal fields.
Selecting a policy reference does not run that policy or prove its decision succeeded.

A registered component declares its input facts, typed output, execution phases and
configuration dependencies. It also declares whether it may continue processing,
terminate with a local result, reject, or request a supported invocation plan. These
are contracts for composition, not permission for documents to load executable code.
Adding a component must not require extending a central application-name enum or
putting its payload fields into every other component's view.

Selection can occur at more than one phase. Early selection establishes deployment
and applicable inspection policy; later body-dependent selection can complete the
operation or destination. Every stage declares which facts it requires.

## 6. Phase and dependency validation

The compiler must reject a dependency cycle or a fact required before it can exist.
In particular:

- A WAF policy needed to inspect the request body cannot depend on facts produced only
  after that inspection. Select an enclosing policy early, or explicitly buffer and
  choose a policy before inspection begins.
- Authorization may establish trusted identity used by later selection; a claimed
  header cannot substitute for that trusted fact.
- Response facts cannot affect an already dispatched initial request. They may inform
  outcome classification for a subsequent permitted attempt.
- Any body wait consumes the request deadline and obeys declared memory limits.

There is no universal ordering of every authentication and WAF operation. The chosen
pipeline must declare dependencies and protection boundaries. For the APIx/Citrus
case, distinguish early identity establishment from APIM access/quota admission.

## 7. Routing plans: Target, Split and Chain

Match selects a routing plan. The executor traverses it. Pick resolves the current
attempt's logical target to a concrete host; Adapt supplies that target's protocol,
model mapping, headers and scoped credential use.

| Node | Meaning |
|---|---|
| Target | One logical destination and its binding/adaptation references |
| Split | Select one child according to declared weights and selection policy |
| Chain | Try children in order, advancing only on configured outcomes |

A chain here is a destination fallback sequence, including supported tool/server invocations. An ordered HTTP filter pipeline is a
separate composition and must not be represented as a fallback chain. WAF is a policy
stage, not an alternate upstream destination. A cache hit or local decision is also
a component outcome; it does not need a synthetic network target. A target binding
names the executor/adapter that supports it; endpoint Pick applies when that binding
actually requires host selection.

Plans may nest splits and chains. Preparation validates references, rejects cycles,
and bounds depth, node count and fan-out. Weights must have a documented numeric
range and at least one eligible positive-weight child. Unavailable branches do not
silently redistribute traffic; that behavior requires an explicit policy.

A split makes its choice once when that node is entered and records it in request
execution state. Retrying the selected target does not resample the split. Fallback
advances the enclosing chain according to its declared policy.

## 8. Retry and fallback

**Retry** attempts the same logical target again, possibly with another host.
**Fallback** advances to another child of a chain. Both consume one request-wide
attempt budget and deadline. A target budget includes its initial attempt.

```yaml
execution:
  maxAttempts: 4
  timeout: 10s
plan:
  chain:
    advanceOn: [connect-failure, unavailable]
    steps:
      - target: primary
        retry:
          maxAttempts: 2
          on: [connect-failure, reset-before-headers]
          perAttemptTimeout: 2s
          backoff: {initial: 50ms, max: 250ms, jitter: true}
      - target: fallback
        retry:
          maxAttempts: 2
          on: [connect-failure]
```

These outcome names need precise host-adapter definitions before implementation.
A reset before response headers does not prove that the upstream performed no work.
The example additionally requires an explicit operation-level replay permission;
otherwise ambiguous failures must not trigger another attempt.

Execution order is: attempt, classify outcome, retry locally if eligible and budget
remains, otherwise advance a chain if its conditions permit, otherwise finish.
A terminal local denial, client cancellation, or invalid authority is not a backend
failure that fallback may evade. Nested plans cannot reset global budgets.

Required invariants:

- **Replay permission:** mutation safety and idempotency are explicit. Idempotency keys
  only help when the destination honors them; cross-provider equivalence is not assumed.
- **Bounded original input:** preserve replayable request input within a declared limit.
  Each attempt is adapted from that input, not from another attempt's mutated bytes.
- **Response commitment:** no transparent retry/fallback after response headers or body
  are committed to the downstream client. Streaming sessions cannot silently restart.
- **Deadlines:** the effective timeout is the earliest caller, policy, authority or
  per-attempt deadline. Backoff and configuration-dependent waits consume time.
- **Cancellation:** stop timers and active attempts, then release request-owned state.
- **Authority:** recheck applicable expiry, revocation and private credential validity
  at each protected dispatch. Capturing a generation cannot bypass these checks.
- **One retry owner:** the host adapter must prevent independent Envoy and Fig retry
  policies from multiplying attempts. Host-level connection retry behavior must also
  be accounted for in the observable budget contract.
- **Accounting:** distinguish logical-request quota from attempt/provider usage. An
  uncertain charged attempt is not automatically refunded or repeated.

Initial scope is sequential attempts. Hedging, parallel branches and stream resumption
are separate designs.

## 9. An open set of consumers

| Consumer | Prepared material | Request-owned state | Replacement/outage concern |
|---|---|---|---|
| LLM routing | Model selectors, plans, target bindings | Selected model and attempt state | Keep related target/adaptation references coherent |
| API access | Route indexes, authentication and entitlement material | Captured decision, deadlines, quota evidence | Recheck authority before dispatch; fence invalidated continuations |
| WAF | Captured rules/CRS and compiled policy engines | Inspection transaction and captured mode | Preserve the selected engine through request/response processing |
| MCP profile routing | Profile selectors, capability exposure and server/tool bindings | Captured profile and any session binding | A profile update must not silently broaden existing authority or retarget an active session |
| MCP tool routing | Qualified tool identities, argument schemas and invocation bindings | Validated call identity, arguments and attempt state | Discovery and invocation must agree; replay safety depends on the tool's side effects |
| Jev | Typed decision definitions, input/output schemas and execution bindings | Validated decision input and evaluation/result state | Validate result type; any escalation or handoff is explicit behavior |
| Guardrails | Versioned policy, input mapping, approved execution binding and result schema | Captured policy, call deadline, transformed content and any stream holdback | Required checks precede protected effects; denial cannot be escaped through fallback |
| Cache | Key extraction, eligibility, partitioning, freshness and invalidation policy | Key, lookup result and any owned fill operation | Preserve tenant/identity isolation; policy replacement is distinct from entry invalidation |

These are illustrative Fig contracts, not claims that all adapters already exist.
Their shared unit is a typed, prepared configuration view; their runtime semantics
remain component-owned.

### MCP profiles and tools

Profile selection can choose the exposed tool set and its server bindings. Tool
selection then resolves a qualified tool identity within that profile, validates its
arguments and selects an invocation binding. A JSON-RPC method alone is insufficient:
a tool invocation also needs its tool name and the authorized profile context.

An implementation must define consistency between advertised tools and invocation:
use a captured catalog/profile version or an explicit revalidation rule. Sessions
require declared update, expiry and removal behavior; per-message capture alone
cannot safely define session affinity. Retrying a tool call requires explicit replay
permission even when the transport method is the same for read-only and mutating tools.
Discovery does not confer invocation authority. User arguments cannot select arbitrary
servers or private credential scopes.

### Jev and other decision components

Selection can choose a typed decision definition and an execution binding. The
component owns evaluation, input/result validation and observations. Evaluation may
be local or use a declared adapter; Fig does not assume that every decision is an LLM
call. Escalation to another component, if desired, is an explicit composition contract,
not an implicit response to any evaluator error.

### Cache

The serializable view describes cache behavior, not the mutable cache contents. A
cache stage owns lookup/fill state, capacity and concurrency separately from view
publication. Replacing a view does not automatically empty, preserve or reinterpret
entries: key-version and invalidation semantics must specify that behavior.

Cache keys declare tenant, identity/authorization partitioning where required,
operation identity and relevant input facts. Eligibility and freshness are explicit.
A local cache hit must still satisfy required access and protection gates; it cannot
bypass them because no upstream dispatch occurs. Coalesced fills need ownership rules
so cancellation by one waiter does not incorrectly cancel work owned by other waiters.
Stale-while-revalidate, negative caching and semantic caching require separate declared
policies; none is implied by the existence of a cache stage.

### Guardrails and bring-your-own implementations

Guardrails are typed component stages. They may inspect model inputs/outputs, tool
arguments/results, retrieved content or other declared data. Their purpose and input
schema are not limited to text moderation or a particular model provider.

The specification separates:

- **Binding:** approved local implementation or remote service, versioned protocol,
  capability declarations, transport trust, and private credential references.
- **Policy:** versioned provider-specific parameters and permitted decisions/mutations.
- **Attachment:** selection conditions, execution phase, required versus optional
  enforcement, order, content mapping, timeout and failure policy.

An illustrative attachment (names are proposed, not LiteLLM wire fields):

```yaml
guardrail:
  bindingRef: tenant-a/content-check
  policyRef: tenant-a/content-policy-v3
  phase: before-invocation
  inputMappingRef: chat-content-v1
  required: true
  timeout: 300ms
  onUnavailable: reject
  permittedOutcomes: [allow, block, transform]
```

Selection can choose an approved attachment using trusted tenant/profile/operation
facts. Caller-supplied guardrail names cannot remove mandatory guards, choose arbitrary
network endpoints or grant access to another tenant's policy. A binding can reference
a registered local implementation or an authenticated remote endpoint. Remote HTTP
adapters are the initial BYO integration candidate; deployed implementation artifacts
are separate from streamed configuration and cannot be uploaded as arbitrary code
inside a spec.

**BYO lifecycle:** management receives a scoped registration, validates ownership,
endpoint/data-sharing permissions and policy, then approves an exact version for
publication. Preparation resolves that approved binding and checks capabilities.
Revocation/removal follows explicit retirement rules. Fig's runtime consumes approved
resources; it does not introduce a second approval or permission service. Deployment
policy can govern trusted operator-authored bindings without requiring a particular UI.

**Results:** normalize supported adapter responses into allow, block, or a bounded
typed transformation. Transport failure, timeout, malformed results and unsupported
content are execution errors, not successful allows. The proposed default for a
required guard is rejection on these errors. An explicitly authorized observe-only
or fail-open policy must be visible in the spec and observations. Policy denial is
terminal for the protected operation; retry/fallback cannot search for a target that
omits the guard.

**Content mapping:** extraction declares the exact fields and modalities sent for
inspection. Transformations identify their source locations and permitted fields;
validate the result before applying it. Preserve call IDs and protocol structure.
Do not flatten tool arguments or structured messages into text and assume a safe
inverse mapping. Cap request/result sizes and use allowlisted metadata. Private
credentials and unrelated request data are not forwarded by default.

A transformation creates a new request representation with provenance. Invalidate or
recompute facts, cache keys and authorization decisions that depended on changed fields.
Do not let a rewrite silently select a different tool, target or trust scope. If such
changes are supported, revalidate their dependencies with bounded execution; never
introduce an unbounded re-selection loop.

**Execution phase and commitment:**

| Placement | Required contract |
|---|---|
| Before invocation | Finish required input checks before dispatching the protected call |
| Before tool execution | Check the final validated tool arguments before side effects |
| Before result release | Inspect or transform a buffered result before downstream commitment |
| Streaming release | Declare chunk/window semantics, bounded holdback and cross-chunk state |
| Observation after release | Audit only; cannot claim to have prevented delivery or side effects |

Running a check concurrently with upstream work cannot guarantee prevention of
upstream processing. Whole-response safety requires bounded buffering before release;
chunk checks cannot imply whole-response guarantees. Once bytes are released they
cannot be recalled. A later streaming violation may terminate the stream using the
protocol's supported mechanism but must not be reported as prevention of earlier delivery.
Unsupported phase/modality/streaming combinations fail preparation rather than silently
skipping a required guard.

**Composition:** mandatory input guards run before any protected invocation, including
fallback targets and tool calls. Required output checks also cover cached/local results.
Declare whether the cache stores original or transformed content and bind reusable
validation evidence to content, tenant/scope, policy version and required context.
A cached allow from an older policy cannot silently bypass current enforcement.

Guardrail calls consume bounded execution time and have their own limited retry policy;
they are accounted separately from model/tool attempts and cannot recursively invoke
unbounded guardrail pipelines. Reusing an input decision across attempts requires
identical relevant content, scope and policy; target-specific changes require rechecking.
Independent pure checks could later run in parallel, but transforming checks require
explicit order and defined dependencies.

For LiteLLM custom guardrails, an adapter can host an existing `CustomGuardrail`
implementation and translate its `apply_guardrail` inputs, returned modifications and
intentional rejections. A deployed Python adapter service or registered native
implementation must declare its supported phases and modalities. Unexpected exceptions
remain execution errors. The configuration references that implementation; it does not
contain Python source or require Fig to load arbitrary classes.

A LiteLLM Generic Guardrail API adapter is another candidate compatibility path, with explicit
mapping of supported capabilities into these contracts. This is not a promise of Python
plugin compatibility or complete protocol support. [RATIONALE.md](RATIONALE.md#decision-10-bring-your-own-guardrails-through-explicit-bindings)
distinguishes the referenced LiteLLM concepts from Fig's proposed behavior.

### WAF and shared lifecycle requirements

WAF policy content and runtime enforcement mode are distinct resources. A mode
selection must reference the exact prepared policy revision it applies to. Detection-only
still performs inspection. Cold/unavailable required protection must not be bypassed.

A temporary transport outage, malformed replacement, stale authority, explicit resource
removal and application shutdown are different events. Each resource contract must
state whether existing requests may finish and whether new requests may be admitted.
There is no universal "retain forever" or "expire after three seconds" view policy.

Request lifetime retention needs an explicit release operation or equivalent ownership
mechanism so old engines, files and host handles can be reclaimed once unused. Retained
versions and long-lived requests need bounds.

## 10. Publication and consistency

Independent resources can be delivered independently. Coupled resources require a
coherent activation boundary: stage all required versions, validate their references,
and publish one generation. Each request captures that generation before relevant
asynchronous waits. A captured generation preserves interpretation; an authority gate
can still invalidate its permission to dispatch.

A possible serializable generation manifest references exact `(type, name, version)`
dependencies. Its final shape remains open. The runtime must retain referenced staged
versions until activation or bounded rejection, rather than keeping only the newest
resource and losing a version an activation needs.

Removal must be explicit. An omitted item in a delta is not deletion. Removing a
required resource cannot silently convert protected traffic into unprotected traffic.
Replacement and rollback must not resurrect retired identities or revoked authority.

## 11. Delivery boundary

Embedded bytes, mounted files, polling and remote streams feed the same validation and
preparation path. A local snapshot adapter can supply a complete resource set; a remote
adapter can supply named updates and removals. Delivery granularity does not determine
activation granularity.

An xDS-like subscription model is a candidate: resource type plus names, versions,
removals, ACK/NACK and reconnect state. Actual Delta ADS versus a smaller protocol is
**not decided**. Neither choice makes EG serve custom Fig resources automatically.
A streamed JSON document is also not automatically an xDS resource or a JSON patch.

Keep EG topology/filter bootstrap separate from high-churn application resources.
Kona supplies evidence for a separate mTLS channel, currently via HTTPS polling; it
has not established a streaming API or implementation for Fig.

Delivery authentication must authorize scope, type and name. A user-supplied node ID
or resource name is not an authenticated identity. TLS issuance, trust distribution,
application publication authority and resource activation remain distinct concerns.

## 12. Observation and verification

Report separately: desired, delivered, accepted/prepared, active, and verified by
traffic. Acceptance is not proof of activation. Record resource/generation references,
selection identifiers, target and attempt number, outcome, and retry/fallback reason.
Do not include raw credentials, request bodies or sensitive extracted facts by default.

Before selecting a wire protocol, executable examples must establish:

- LLM body selection, HTTP operation selection, and early WAF policy selection.
- MCP profile/tool selection with consistent advertised and invoked tool identities,
  argument validation, session update rules, and no replay of non-replayable calls.
- Jev typed input/output selection without requiring LLM execution.
- BYO guardrail registration/approval isolation, allow/block/transform/error handling,
  required checks on cached results and fallback attempts, bounded transformations,
  cross-chunk behavior, and no claim of blocking after content has been released.
- Cache key isolation, explicit invalidation, and local hits that preserve required
  access/protection gates; mutable cache entries remain outside immutable views.
- Missing versus pending facts, malformed bodies, bounds and ambiguous selection.
- Invalid preparation leaves active state untouched; cross-view activation is coherent.
- An update during body wait cannot mix generations.
- WAF request/response processing retains its selected engine; mode binds to its policy.
- Revocation or stale authority prevents dispatch even with a captured view.
- Split choice stays fixed across retry; fallback and retries share global budgets.
- Non-replayable requests, committed responses and cancellation never retry.
- Explicit removal fails required protection closed and eventually releases resources.
- Real host tests prove backend receipt/non-receipt as well as client response.

## 13. First design slice and exclusions

First specify a representative matrix and its expected event traces: LLM model
routing, API operation selection, WAF policy/mode selection, MCP profile/tool routing,
Jev decision selection, cache lookup/local completion, and BYO guardrail enforcement. This matrix exercises the
abstraction; it does not require implementing every adapter in the first slice.

Then define typed extraction, selection results, component outcomes, prepared-view
lifetime and sequential plan execution. Include at least one local completion case
alongside upstream dispatch so the contract does not accidentally require forwarding. Only then
choose transport schemas and a host adapter.

This proposal does not implement a general workflow engine, arbitrary uploaded code,
a new management permission system, a universal policy payload, or production PKI.
It does not promise compatibility with current Plum, APIx, Citrus, xDS or Gateway API
until the corresponding adapters and behavior have been qualified.
