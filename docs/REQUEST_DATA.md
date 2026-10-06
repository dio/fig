# Request data and typed module handoffs

Status: first header-time WAF → Marker implementation, informed by the
[source survey](surveys/REQUEST_DATA.md). Scope: local Envoy, ordered downstream HTTP
modules. The broader composition contract below remains a design target.

## Decision

Fig defines a host-independent contract for publishing and consuming small
request-local values. The initial Envoy carrier is a bounded serialized envelope in
raw-byte filter state. The producer owns the value's schema; the host adapter owns
copying, storage and callback lifetime. Match continues to evaluate ordinary facts.

Distinguish three things:

1. Parsed inputs: immutable Body document, owned locally by its parsing context.
2. Extracted facts: e.g. a model string, bound to a representation and extractor profile.
3. App results: e.g. WAF selection or inspection outcome, with an app-owned schema.

Filter state carries serialized facts/results in the first slice. It does not carry
Go pointers, prepared engines, credentials, raw bodies or arbitrary mutable maps.
Publishing a model fact saves repeated extraction for a consumer; it does not make
an entire parsed tree available without parsing again.

## Scheduling: ordered modules, local phases

For `[waf, marker]`, WAF runs its own Match and inspection before Marker runs its
own Match and action. There is no global pass that runs every app's Match first.

```text
WAF headers: Match → inspect → publish outcome → continue
                                              |
Marker headers: read declared outcome → Match → apply marker
```

A terminal WAF reply stops request processing before Marker. Publishing a block
outcome does not cause Marker to execute; a separately declared observer could read
that value only in a callback phase whose availability is qualified.

Availability must be checked against actual callbacks, not just list order. A body
producer cannot satisfy a later filter's header-time consumer unless the host proves
that it withholds the later headers until publication. Initial handoffs are synchronous
and at request headers. A body-phase handoff is a separate follow-up with buffering,
trailers, pause/resume and abort tests. Response callbacks need a separate dependency
order; downstream request order is not a proof of response-time availability.

## Declared contract

Each configured producer exposes named slots. Each consumer explicitly binds an input
to one producer instance/slot, type version and required phase. No global latest-value
lookup and no implicit access to another app's private state.

Illustrative attachment fragment, not an accepted schema:

```json
{
  "instances": [
    {
      "id": "waf-a",
      "app": "fig.waf/v1alpha1",
      "exports": {"inspection": "fig.waf-outcome/v1alpha1"}
    },
    {
      "id": "marker-a",
      "app": "fig.marker/v1alpha1",
      "inputs": {
        "inspection": {
          "from": "waf-a",
          "slot": "inspection",
          "type": "fig.waf-outcome/v1alpha1",
          "phase": "request-headers",
          "required": true
        }
      }
    }
  ]
}
```

The installed WAF contract defines the outcome fields; authored config cannot invent
exports. A typed projection can expose `matched: boolean` to Marker. A generic input
binding decodes the producer's declared schema and produces ordinary typed facts;
Marker's app logic does not import WAF. Such a projection needs an installed schema
or a validated scalar record contract, not arbitrary expression execution.

Preparation rejects missing/ambiguous producers, duplicate instance IDs/slots,
unsupported type versions, cycles, phase/site incompatibility, unbounded outputs and
required consumers after producers that can skip without an explicit resolution.
An optional consumer may treat absence as Missing only if its contract says so;
corrupt, wrong-type or stale data is always an error, never an optional skip.

## Envelope and identity

One nonempty envelope per producer slot, containing:

- Envelope version and payload type/version.
- Placement/execution identity and producer instance/slot.
- Activation ID for the composed chain, plus producer scope and generation.
- Input representation ID and producer phase.
- An explicit value or a declared no-result outcome; never empty bytes as a sentinel.
- App-owned payload, validated with bounded decoding and exact scalar types.

The composition compiler derives filter-state keys from deployment-owned instance
identities, using unambiguous encoding under a reserved Fig namespace. Request input
cannot supply keys, producer IDs or expected generations. Consumers receive exact
bindings from prepared configuration and compare the envelope against them.

WAF and Marker may have different app generation tokens. Do not require their tokens
to be equal: the composition lists each expected token under one activation identity.
That identity detects mismatches; it does not itself provide atomic activation or
resource retention across independently configured factories. First qualification
uses static configuration. Hot updates require the separate activation protocol.

Representation identity describes the input actually evaluated, not necessarily raw
client headers. For example, `request-headers@waf-entry` differs from post-rewrite
headers. A mutation invalidates reusing old facts as facts about the new representation.
It does not erase historical outcomes. Consumers must say which representation they
require; the system must not silently reinterpret a previous selection.

## Publication and reading

Carrier API (the envelope and bindings are independent of the SDK):

```text
Publish(preparedSlot, typedValue) -> success | conflict | invalid | limit | storeFailure
Read(preparedBinding) -> value | missing | mismatch | invalid
```

Slots are single-writer, publish-once. Duplicate publication is a conflict even for
equal data, making accidental callback repetition visible. The adapter checks for a
prior entry, validates and serializes the entire envelope, performs the write, then
reads back and compares bytes. The pinned Go setter discards its ABI success result,
so a write cannot be assumed successful without read-back.

This is a protocol among trusted filters executing serialized stream callbacks, not
native atomic compare-and-swap or protection from malicious in-process modules.
A wrong native object type can look absent to the raw-byte getter; namespace ownership
and exclusion of competing non-Fig writers are admission prerequisites. If these
cannot be established, refuse the composition. A future SDK binding with reliable
status/existence checks would strengthen diagnostics.

Readers check size before copying borrowed bytes, copy within the callback, validate
the envelope and expected identity, and decode into owned values. No borrowed buffer
or native handle escapes to a background goroutine. Never fetch filter state from a
raw goroutine; any future asynchronous integration must use qualified host scheduling.

A producer's local state is Unpublished → Published or Failed; a cancelled/ended
stream cannot publish. A required failed publish or read terminates processing with
a configured internal error (initial fixture: 500), before any protected downstream
action. Invalid client JSON remains an input error, not a handoff transport error.
Already-committed responses must be reset/terminated according to the host contract,
not followed by a second reply. Initial scope avoids response-side publication.

## Lifetime, trust and limits

The pinned raw-byte API uses FilterChain lifetime and no upstream-connection sharing.
Treat the envelope as local to that chain invocation. Do not promise survival across
stream recreation, retries, upstream attempts, connection reuse or another proxy.
Connections can carry concurrent HTTP streams; keys identify slots within each
stream's state, not a global cache. Responses/completion access need explicit proof.

Envoy owns stored byte copies. Consumers own any copies retained after a callback.
Use retained app factory configuration for decoding; do not consult a mutable latest
schema at read time. Filter-state bytes are not configuration leases or authorization.
Required protection cannot be bypassed by a diagnostic header, a model fact or a
previous successful result. Do not put secrets or full request payloads in handoffs;
filter state can be exposed by explicitly configured loggers. Metadata should carry
only bounded diagnostics such as slot/outcome/error code.

Implemented limits: 4 KiB per encoded envelope including identity, 16 scalar fields,
1024 bytes per string value, 128 ASCII characters per identity/field name, decode
depth 8 and 256 nodes. Fixture admission caps the chain at 16 declared stages, giving
a conservative 64 KiB stored-envelope bound. Directly authored Envoy configurations
can bypass that composition cap; adapters enforce the per-envelope limit only. These
are allocation bounds, not performance qualification.
The compiler can conservatively sum slot maxima, avoiding a mutable global byte
counter across filters. Decoder allocations and per-consumer copies also consume
memory; impose a bounded consumer count and measure actual memory in qualification.
Never silently truncate data or fall back to an untyped carrier.

## Package and dependency boundaries

Implemented packages:

- Root `handoff`: envelope, prepared bindings, bounded codecs and errors; no SDK or apps.
- `hosts/envoy/handoff`: independent Go module implementing the carrier with the SDK,
  depending on root core only. It must not import the composing host module or apps.
- App Envoy adapters: publish/consume their declared values using the carrier.
- `hosts/envoy/cmd/fig-envoy`: concrete registration/composition; no app switch in the carrier.

No new generic app execution interface is needed to prove one producer and consumer.
Keep WAF/Marker as separate modules. Avoid a module dependency cycle by making the
carrier a nested independent module, not a package in the executable's app-dependent
Go module. Match receives projected facts through its existing input-field seam.

## Parsed-body sharing is a separate decision

The first handoff transports selected scalar facts or compact app outcomes. Body
still parses once per owning filter. To share the actual immutable document later,
choose an explicitly owned same-runtime context or add a qualified Go binding for
opaque filter-state objects with handles, destructors and leases. Do not build an
ad hoc process-global pointer map indexed by headers or metadata.

Jev's inspected HTTP API includes a JSON model field. It can consume a model fact
with compatible parser profile/representation, then perform its own State/Questions
validation. Sharing `/model` does not imply a valid Jev request or replace WAF parsing.

## Local qualification

The first implementation provides WAF → Marker header outcome sharing. Independent
Marker behavior is preserved when no input binding is configured. No EG, Kubernetes or remote
channel is involved. Use real separate filter instances in one local Envoy, not only
an in-memory mock or diagnostic headers.

| Case | Required evidence |
|---|---|
| Clean / detect-only request | Consumer decision changes from the actual producer value |
| Enforcing WAF block | Marker does not execute; no backend receipt |
| Two WAF instances | Distinct slots and generations; no accidental overwrite |
| Missing required producer | Preparation rejects when known; runtime fails closed otherwise |
| Missing optional slot | Explicit Missing behavior only |
| Wrong type/version/producer/generation/representation | Reject, no fallback |
| Corrupt, duplicate-key, oversized envelope | Bounded rejection before consumer action |
| Duplicate publication / failed write | Explicit error; include read-back failure injection |
| Client spoofs diagnostic headers | No effect on handoff or selection |
| Concurrent requests and reused connection | No value or generation leakage |
| Source/returned byte mutation | Published/decoded values remain independently owned |
| Reset, local reply, cleanup | No late access/publication or retained registry entries |

Follow-up gates: body producer/consumer pause order, trailers and fragmented bodies;
response-time reads and teardown order; recreated streams and upstream attempts;
separate shared libraries if supported; hot configuration changes. Until tested,
these remain unsupported rather than inferred from successful request-header sharing.

## Implemented configuration and admission boundary

The current transport wrapper remains `{entry, bundle}` with optional `export` on
WAF and optional `input` on Marker. These bindings belong to trusted composition
configuration, outside app bundles. The `instances` example above is still a future
composition schema. Actual WAF `export` value:

```json
{
  "activation": "local-1",
  "placement": "edge",
  "producer": "waf-a",
  "slot": "inspection",
  "type": "fig.waf-outcome/v1alpha1",
  "scope": "demo",
  "generation": "1",
  "representation": "request-headers@waf-entry",
  "phase": "request-headers"
}
```

Marker `input` contains that same object under `binding`, plus `field: "matched"`,
`fact: "input.inspection"`, `kind: "boolean"` and `required: true`. The consumed
[Marker bundle](../examples/config/marker-inspection.json) extracts that admitted
input field and selects `waf-clean` or `waf-detected`. It requires this binding;
it is not a standalone replacement for the independent Marker example.

WAF owns `fig.waf-outcome/v1alpha1`, currently the scalar fields `matched` (boolean),
`action` (string) and `policy` (string). Its adapter checks export type, slot,
representation, phase and producer bundle scope/generation at configuration time.
The representation is the header input inspected after diagnostic headers are stripped.
Marker consumes a generic scalar projection and imports no WAF code.

`handoff.ValidateChain` checks ordering, exact identities, duplicate slots and stage
count. Optional inputs may lack a producer entirely; a declared producer must precede
the consumer. The fixture renderer invokes this check in template order. It does not
parse arbitrary Envoy topology or implement the future activation compiler, registry,
app output descriptors or full schema compatibility checking. Projection field names
and scalar kinds are checked against the decoded record at runtime. Direct Envoy
configuration bypassing the renderer still receives adapter validation and runtime
identity checks; a missing required input or malformed record produces HTTP 500.

Keys hash placement, producer and slot under `fig.handoff.`. Activation, generation,
type and representation are checked inside the envelope, so stale values cannot be
accepted as a fresh result. Publishing twice conflicts even when payloads match.
Optional absence leaves the Match input missing; optional corruption still fails.

Qualification completed on the pinned native macOS arm64 Envoy:

- Real WAF → Marker clean/detect selection, block short circuit, spoofed diagnostic
  headers, 40 concurrent requests, and two producer instances with different results.
- Native runtime rejection of missing required values, stale generation, wrong type,
  missing projection fields and duplicate publication; optional absence skips marking.
- Unit tests for all identity fields, malformed/oversized envelopes, read-back failure,
  copied ownership, invalid bindings and callbacks after stream completion.
- Race tests and vet across all seven modules, plus a bounded decoder fuzz run.

Two-producer native coverage uses distinct instance identities with the same generation;
identity mismatch coverage separately checks generation. Reset/recreation, HTTP/2,
response/body handoffs, separate libraries and live activation remain unqualified.
There is no process-global request registry or shared parsed-body object.
