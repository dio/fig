# Match primitive

Status: proposed first primitive, 2026-10-06. This is a contract and implementation
slice, not a stable wire schema. Examples illustrate semantics. A subsequent
[Go implementation](../../match/README.md) implements a bounded subset; its README records
the differences and missing runtime/host integration.

## 1. Responsibility

**Match extracts declared facts and deterministically selects a typed value or
reference from a captured prepared view.** It does not execute the selected behavior.

Use the same primitive for early WAF policy selection, HTTP operation selection,
LLM routing-plan selection, MCP profile/tool bindings, Jev definitions, cache policies
and guardrail attachments. Consumers define their result types; Match has no central
application enum or required provider/model fields.

The primitive has three parts:

1. Serializable extraction and selection specifications.
2. Preparation into an immutable, typed view.
3. Request-owned evaluation against that view.

The host adapter supplies event inputs and publishes typed results. The view runtime
owns activation and retention. Neither responsibility belongs in the selector.

## 2. Boundary

| Match owns | Another component owns |
|---|---|
| Fact declarations, extractor arguments and typed predicates | Authoring API translation and publication authority |
| Preparation/type checking and deterministic rule order | Distribution, coherent generation activation and retirement |
| Fact state and selected result for one evaluation | Envoy callbacks, buffering, timers and local HTTP replies |
| Dependency and capability requirements | Composition ordering and host feasibility checks |
| Selected policy/plan/binding reference | WAF inspection, authorization, guardrails, cache lookup |
| Selection diagnostics without raw sensitive values | Plan traversal, split randomness, retries, Pick and Adapt |

A match is not an authorization grant. A client-provided tenant header remains an
untrusted claim even when a rule matches it. Trusted identity must originate from a
declared trusted producer, and protected actions retain their admission checks.

## 3. Serializable specification

A Match specification contains:

- Identity and schema version, with resource revision separate from schema version.
- Evaluation phase and input representation (for example, original request headers).
- Named facts: registered extractor/version, source, arguments, result type,
  normalization, bounds and sensitivity.
- One registered output type/version and an ordered list of uniquely named rules.
- Typed predicates and literal typed results or scoped resource references.
- Explicit no-match behavior: return `NoMatch`, or return a typed default result.

The compiler resolves references within the captured generation's scope. A result
cannot construct arbitrary resource names, endpoints or credential scopes from input.
The initial slice returns validated literal results/references; dynamic result
construction can be added when a concrete consumer requires it.

Illustrative early WAF selection:

```yaml
schema: fig.match/v1
name: edge-waf
phase: request-headers
representation: original-request
facts:
  host:
    extractor: http-authority/v1
    args: {component: hostname, normalization: ascii-lowercase}
    type: string
outputType: waf-policy-selection/v1
rules:
  - id: public-api
    when: {equals: {fact: host, value: api.example.com}}
    result: {policyRef: public-api-policy}
onNoMatch:
  result: {policyRef: baseline-policy}
```

The consumer's output schema declares `policyRef` as a resource dependency; the
compiler resolves it to an exact prepared revision. The default undergoes the same
validation as every rule result. This example does not define enforcement mode:
that can live in the selected policy attachment or a coherently bound mode resource.

Illustrative body-dependent selection:

```yaml
schema: fig.match/v1
name: chat-plan
phase: request-body-complete
representation: original-request
facts:
  model:
    extractor: json-pointer/v1
    args: {pointer: /model, normalization: none}
    type: string
    limits: {maxBodyBytes: 65536, maxDepth: 32}
outputType: routing-plan-selection/v1
rules:
  - id: support
    when: {equals: {fact: model, value: support-chat}}
    result: {planRef: support}
onNoMatch: {return: no-match}
```

A separate attachment controls eligibility, such as POST on the chat-completions
endpoint, and maps `NoMatch` to an application response. It must gate body collection
for unrelated traffic. Selecting a plan does not choose a split branch or provider.

## 4. Fact semantics

Each fact is in exactly one state:

| State | Meaning |
|---|---|
| Pending | Its declared source phase has not completed |
| Present(value) | Extraction completed with a value of the declared type |
| Missing | Extraction completed and the source value is absent |
| Invalid(code) | Input violates extraction syntax, type, multiplicity or bounds |

For the first slice, every declared fact must complete before the stage evaluates
rules. An invalid fact makes the evaluation `Failed`; it cannot become a no-match or
default. A missing fact remains missing. `equals(missing, value)` is false and
`exists(missing)` is false; `isMissing` explicitly tests absence. No implicit empty
string, zero, null or type coercion is supplied. Consequently `not(equals(...))` can
be true for a missing fact; configurations requiring presence must include `exists`.

Predicates initially support typed `equals`, `in`, `exists`, `isMissing`, `all`, `any`
and `not`. Boolean operators operate on completed, valid facts only. No regex,
scripting, network lookups or user-uploaded implementations in the initial slice.
Restrict initial values to strings, booleans and bounded signed integers; a JSON null
for one of these types is invalid, distinct from an absent JSON pointer.

Extractor versions define exact semantics. The initial HTTP adapter must specify:

- Method: case-sensitive token, no automatic uppercasing.
- Authority hostname: parsed authority, explicit port handling, ASCII lowercase only
  when declared; malformed values fail. No implicit IDNA conversion.
- Path: path component without query; no percent-decoding or slash normalization.
- Named header: case-insensitive field name, single-value extraction; duplicate values
  fail unless a future extractor explicitly defines multiplicity.
- JSON pointer: complete bounded JSON document, unique object keys, bounded nesting,
  exact result type; no implicit string trimming. The initial adapter accepts declared
  JSON media types and identity content encoding; unsupported encoding fails.

These are proposed Fig semantics, not a claim of compatibility with every gateway.
Input size and collection limits must be checked before allocating an unbounded body
or header value. The installed extractor registry validates arguments and reports
requirements; configuration cannot invent capabilities or trust levels.

## 5. Selection and results

Rules use **ordered first-match**. Overlap is legal; earlier rules win. Compiled indexes
must preserve list order. Duplicate rule IDs, unknown facts, incompatible predicates,
unknown output types and unresolved result dependencies fail preparation.

An evaluation returns one of:

| Result | Payload and meaning |
|---|---|
| Waiting | Required phase/input is pending; no selection published |
| Selected | Typed immutable value, rule ID or default marker, view/generation identity |
| NoMatch | All rules false, no default configured |
| Failed | Stable diagnostic code for invalid input, limits or extractor failure |
| Cancelled | Request ended before completion |

`Waiting` is nonterminal; the other results are terminal. Completion occurs at most
once. A failed or cancelled evaluation cannot later resolve to a selection. Raw input
and private data are not included in diagnostics by default. The adapter, rather than
the core, maps terminal results into HTTP status/body or another protocol action.

Consumers declare which result they require and how terminal failure is handled.
Required WAF protection cannot interpret `NoMatch`, `Failed`, or a missing producer
as permission to continue. An optional attachment may explicitly skip on `NoMatch`.

## 6. Phase, representation and ownership

Use separate instances for separate decisions:

```text
headers --> early Match --> WAF policy selection --> WAF request inspection
body complete --> late Match --> routing plan selection --> executor/Pick/Adapt
```

This diagram shows dependencies, not a prescribed filter callback list. The host must
prove that buffering and callback order can realize them. An early WAF selector cannot
depend on the body that the selected WAF policy must govern.

Capture one coherent generation before extraction begins. Early and late stages that
share dependencies use that same request generation. Never reload the active pointer
when the body finally arrives. Each evaluation holds a lease, or shares a request
lease, until its selected result and downstream consumers no longer need the view.
Result references cannot outlive their retained prepared resources.

Facts belong to an explicit input representation. If a guardrail or adapter transforms
that representation, affected facts become invalid for the new representation. Start
an explicitly composed new evaluation when needed; do not mutate an already published
selection or silently loop back through Match. Attempt-specific Match instances are
possible later with separate ownership, not implicit reuse of client-stream facts.

The core is host-independent and performs no I/O. An extractor reads bounded input
provided by its adapter. The HTTP adapter owns body accumulation, deadline/cancellation,
continuations and trusted handoff storage. It must not retain borrowed native buffers
past their callback lifetime. Multiple consumers need a compatible shared buffer
contract; Match must not consume or alter bytes that WAF or forwarding still needs.

View retirement and revocation are runtime/admission events. They do not turn a
previous `Selected` result into proof of continuing authority.

## 7. Proposed implementation seams

Conceptual operations, not final Go signatures:

```text
Prepare(spec, extractorRegistry, outputRegistry, dependencyResolver)
    -> PreparedMatch + Requirements | preparation errors
Begin(capturedView, representationID)
    -> Evaluation
Advance(evaluation, boundedPhaseInput)
    -> Waiting | Selected | NoMatch | Failed
Cancel(evaluation)
    -> Cancelled, unless already terminal
Release(evaluation/result ownership)
```

`PreparedMatch` owns compiled extractors, predicates, typed results and dependencies.
`Evaluation` owns fact slots and its terminal state. Registries are installed code;
specifications are data. A small Go core can use typed output adapters without placing
WAF/LLM/MCP fields into a universal result struct. Thread-safety belongs to the owner:
serial host callbacks may serialize an evaluation, while cancellation races require
an explicit once-only completion mechanism.

Do not require a generic scheduler, transport client or public CRD to build this core.
A serializable specification and its in-process prepared representation are distinct.

## 8. Migration from Plum

The inspected `pipeline/match/match.go` combines endpoint eligibility, JSON model
extraction, a global model map, provider-specific output, filter-state publication,
async promise completion and OpenAI-shaped error responses.

| Plum behavior | Fig destination |
|---|---|
| Method/path eligibility in `handler` | HTTP attachment or early selection spec |
| `requestModel` | Registered JSON-pointer extractor with explicit normalization |
| `View.Models` and `ResolveModel` | Prepared ordered selection; exact-key index as optimization |
| Provider fields in `Decision` | LLM-owned output/binding schema |
| `Decision.Apply` and stream promise | Host handoff/continuation adapter |
| `sendError` | LLM HTTP error mapping |
| `SetView` and lookup of current view at body completion | View runtime plus capture before body wait |

Plum trims the model string and resolves against the active view at body completion.
A compatibility adapter must explicitly request trimming if preserving that behavior;
Fig's generation capture deliberately changes update semantics. Do not silently claim
a drop-in migration. No Plum source changes are part of this design slice.

## 9. Smallest implementation sequence

1. Implement preparation and pure selection over supplied typed facts. Cover WAF
   policy, routing plan and one non-routing result type without application switches.
2. Add versioned extractors and explicit fact states. Exercise header-only and bounded
   JSON input through an in-memory adapter before native host integration.
3. Add request capture/ownership integration and a single HTTP adapter. Prove an update
   during body wait cannot mix views and cancellation cannot publish late results.
4. Connect early WAF policy selection and late LLM plan selection in one host deployment.
   Check shared buffering and mandatory protection before expanding transport support.

Future implementation acceptance cases: first-match overlap; pending never defaults;
missing versus empty/null/invalid; duplicate headers/JSON keys; size/depth bounds;
invalid default references; scope isolation; retained revisions across reload; terminal
completion races; transformed-input invalidation; no-match mapping; actual downstream
and upstream effects. These cases are specified here, not executed in this docs change.

Deferred: general expression languages, regex, lazy predicate-dependent extraction,
body streaming queries, arbitrary computed outputs, transport protocols, plan execution
and a universal scheduler. The main risks are host buffer ownership, phase dependency
cycles and lifetime mistakes; the pure predicate evaluator is the smaller part.

## Body parsing boundary

See [Body input and parsing](BODY.md) for the proposed shared parsed view and
versioned extractor migration. `json-pointer/v1` remains a per-fact compatibility wrapper. The new
`body-json-pointer/v1` reads the immutable `Input.Document`; Match continues
to own typed facts and selection, while the host owns collection and cancellation.
