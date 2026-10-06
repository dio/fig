# Body input and parsing contract

Status: partially implemented. `body` provides immutable bounded JSON documents;
`json-pointer/v1` is now a compatibility wrapper and `body-json-pointer/v1` reads a
shared `match.Input.Document`. The native fixture parses once for model and stream
facts. WAF and Marker remain header-only. The resource/reference schema below and
cross-app sharing are still proposals, not accepted bundle configuration.

## Ownership

```text
placement selects app instances
  → app requirements declare needed input and phase
  → host collects bounded original bytes
  → Body parses one immutable representation
  → extractors read typed facts
  → Match selects an app-owned result
  → app executes or returns a terminal action
```

| Layer | Responsibility |
|---|---|
| Placement | Which configured instances execute, and at which sites |
| App compiler | Required body profile, fact types, dependencies and failure policy |
| Host adapter | HTTP framing, media/encoding admission, bounded collection, pause/resume, deadlines and cancellation |
| Body primitive | Pure bounded parsing, immutable document access and structured errors |
| Extractor | Read a declared path from a named body view; missing/type semantics |
| Match | Predicates and ordered selection over facts |
| App | Inspection, adaptation or other behavior; mapping failures to actions |

Body does not own HTTP handles, networking, app outputs, routing or local replies.
Parsing is a declared dependency, not an unconditional global filter that buffers
all traffic. Header-only WAF and Marker must not acquire body buffering requirements.
A required body decision must finish before its protected dispatch or local result.

## Serializable requirements

Illustrative resource and fact; these names are proposed, not currently loadable:

```json
{
  "type": "fig.body/v1alpha1",
  "name": "request-json",
  "version": "1",
  "spec": {
    "source": "downstream-request-original",
    "phase": "request-body-complete",
    "parser": "json/v1",
    "mediaTypes": ["application/json"],
    "contentEncoding": "identity",
    "limits": {"maxBytes": 4096, "maxDepth": 32, "maxNodes": 2048}
  }
}
```

```json
{
  "name": "model",
  "extractor": "body-json-pointer/v1",
  "type": "string",
  "args": {
    "bodyRef": {"type": "fig.body/v1alpha1", "name": "request-json", "version": "1"},
    "pointer": "/model"
  }
}
```

Use exact scoped references and validate every dependency before activation. Parser
names resolve only to installed implementations. The compiler rejects body input at
header phase, unsupported representation/site/media/encoding, unresolved references,
cycles, invalid pointers and absent failure mappings. Host hard limits bound authored
limits; a config cannot enlarge the host's admitted memory or time budget.

The first profile accepts exactly one `application/json` Content-Type (parameters
parsed using HTTP media-type rules), absent or identity Content-Encoding, and a
complete document. No implicit `+json`, sniffing, decompression or multipart parsing.
Collection timeout comes from an explicit admitted host contract; it is not a parser
option. No fallback to a different parser after a parse error.

## First parser: complete JSON

- Exactly one JSON value; surrounding whitespace allowed; empty body and trailing
  values invalid. A top-level scalar is valid JSON even when an app requires an object.
- Reject duplicate object keys after decoding escapes. Bound bytes before collection
  and nodes/depth while building the document. Each scalar or container counts as one
  node; a root container has depth one. Object keys are covered by the byte budget.
- Reject invalid UTF-8 and unpaired surrogate escapes instead of silently replacing
  them. This stricter proposed behavior needs explicit regression coverage; it is not
  claimed for the current Go decoder implementation.
- Preserve number precision. String, boolean and signed 64-bit integer facts have
  exact types; no trimming, coercion, float rounding or null-to-missing conversion.
  The initial integer extractor accepts only integer lexical forms, as the spike does.
- JSON Pointer uses explicit escaping and canonical nonnegative array indexes.
  Absent paths yield Missing; a present null, object, array or incompatible scalar
  yields Invalid for the initial scalar fact types. A missing pointer is distinct
  from a malformed document.

Parse errors fail the body view. They cannot become Match NoMatch or a default
selection. The app maps errors; the native fixture can retain 400 for malformed/type
errors, 413 for resource limits and 415 for unsupported media/encoding. Cancellation
causes cleanup, not a second HTTP reply. Diagnostics contain codes, not body values.

## Collection, sharing and lifetime

A request captures prepared configuration before it waits for a body. The host owns
one collection state per admitted representation. Its states are Awaiting, Ready,
Failed and Cancelled; terminal completion happens once. End-of-stream in headers,
data or trailers completes collection exactly once. Content-Length is an early check,
not a substitute for counting received bytes. Limits apply before copying each chunk.

Borrowed native bytes must be copied before callback return when retained. Account
for the host's forwarding buffer, Fig's retained bytes and the parsed tree: a 4 KiB
input limit does not mean 4 KiB total memory. Bound concurrent buffered streams and
aggregate bytes in the host as well as each request; specify refusal and deadlines.
Release buffers and document references on completion/reset/cancellation, and retain
any view still needed by an asynchronous consumer until that consumer releases it.

Parse once per request representation and identical parser profile, within an
explicitly shared execution context. Reuse requires equal limits and semantics in
the first slice. Different profiles must not silently raise a stricter consumer's
limit; reject unsupported sharing or execute separately under a total budget.
Do not promise cross-filter sharing merely because apps load the same library.
Independent Envoy filters need a qualified shared context/ownership protocol first.

Expose read-only typed access, not mutable maps or slices. No cache across requests.
The original body bytes remain unchanged for forwarding and consumers that need raw
input. Parsing never reserializes the outbound request. A transformation produces a
new representation identity and invalidates reuse of old parsed views and facts for
that new representation. Retried/upstream attempts have explicit input identities;
request and response parsing are distinct contracts.

## WAF and streaming boundaries

Coraza owns WAF parsing and inspection semantics. A Fig JSON view is useful for LLM
model extraction, MCP method/tool facts and guardrail inputs; it does not replace
Coraza's parser or imply equivalent interpretation. Body WAF must explicitly receive
its raw bytes, content type and lifecycle callbacks in a separate qualified slice.
The currently implemented WAF inspects headers only.

Full buffering cannot be imposed on SSE, long-lived streams, uploads or gRPC framing.
Future incremental parsers need separate contracts for partial facts, bounded windows,
completion, cancellation and backpressure. A complete JSON parser cannot authorize
forwarding based on a prefix while ignoring malformed or disallowed trailing input.
Response parsing and transformations are also outside the first implementation.

## Implementation sequence and acceptance

1. Extract pure JSON parsing from Match into a host-independent Body package. Add
   byte/depth/node limits and typed read-only access. Retain the current extractor as
   a compatibility wrapper; introduce the view-based extractor with a new version.
2. Add a serializable Body resource and dependency validation to one body-aware app
   slice. Do not silently extend header-only WAF or Marker contracts.
3. Adapt the native body-Match fixture to collect once and parse once for two declared
   facts (for example model and stream). Keep app actions separate from parse errors.
4. Qualify multi-app sharing only after proving the host ownership/callback contract.

Acceptance covers malformed/empty/trailing JSON, duplicate escaped keys, Unicode,
null versus absent, integer boundaries, pointer escapes, byte/depth/node boundaries,
fragmented input, trailers, false Content-Length assumptions, deadlines and resets.
Native traffic must prove unchanged forwarding bytes, no backend receipt on failure,
no body collection for header-only traffic, one parse for multiple facts, concurrent
request isolation and retained configuration during a body wait. Property/fuzz tests
should establish no panic and bounded termination within admitted input limits.

Avoid simultaneously implementing a general streaming engine, global app coordinator
and format registry. This slice establishes the body contract and demonstrates it
with one complete JSON consumer before expanding host/runtime scope.

## Implemented first slice

The standalone native Match fixture accepts `bodyParser` with `maxBytes`, `maxDepth`
and `maxNodes`. It requires body phase, equal parser/collection byte limits, and
rejects mixing legacy body extractors with a shared parser. The fixture uses 4096
bytes, depth 32 and 256 nodes. It supplies one document per evaluation; the implemented
`body-json-pointer/v1` accepts only `{"pointer":"/model"}` (or another pointer),
not the future `bodyRef` argument. No bundle resource loader has been added.

Depth/node/byte failures map to 413; malformed JSON, invalid Unicode and wrong fact
types map to 400. This changes the native fixture's previous depth-error response
from 400 to 413. Legacy extractor decoding and fact error codes remain compatible,
including its permissive Unicode replacement behavior. New consumers use strict Parse.

Native evidence: both model and stream facts consume the same document, one parse
is reported in the diagnostic `x-fig-body-parses` header, original bytes reach the
backend unchanged, header-only paths report no parse, errors receive no backend
receipt, and concurrent requests remain separate. The header is stripped from client
input and is diagnostic only. Unit tests cover cancellation and captured generation;
no live generation-update mechanism or aggregate buffering admission is implemented.
Fuzzing and race tests supplement these checks. Deliberate streaming/abort qualification,
body WAF, bodyRef resolution and cross-filter shared contexts remain future work.
