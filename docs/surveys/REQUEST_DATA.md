# Survey: request data between Fig modules

Status: source survey, 2026-10-06. No new data-sharing runtime or native qualification
was implemented in this change. Scope is one local Envoy, downstream HTTP filters.
Fig baseline: `6ba0e771b0f8fdf518ec034c62e99c7017549db0`.

## Findings

| Mechanism | Observed behavior | Fit for Fig |
|---|---|---|
| Per-filter Go fields | Current WAF, Marker and Match retain their own results | Local state; not a cross-filter interface |
| Dynamic metadata | Current apps emit diagnostic maps; SDK can read metadata | Observability; possible serialization carrier, but no Fig schema/ownership contract |
| Raw-byte filter state | Pinned HTTP SDK exposes get/set; C++ copies bytes into StringAccessor | Preferred initial carrier for small typed envelopes |
| Typed filter state | Key must identify an installed Envoy ObjectFactory | Native Envoy interoperability, not automatic registration of Go types |
| SDK SetData/GetData | SDK-local Go registry plus metadata lookup tokens | Possible same-library optimization, with material lifetime restrictions |
| Opaque filter-state objects | Pinned C ABI has object/destructor/lifespan callbacks; Go handle has no corresponding object methods | Future SDK work, not usable through current public Go interface |
| Explicit Go request context | Can own immutable objects and reference lifetimes | Appropriate inside one owner; sharing across independent filter instances needs a bridge |
| HTTP headers | Client/backend-visible and modifiable | Diagnostics only, not internal authority or ownership |

Envoy distinguishes static configuration from per-stream/per-connection dynamic
state. Upstream connection sharing can extend object lifetimes and affect pooling;
we must not opt into it implicitly. See [Envoy's data-sharing overview](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/data_sharing_between_filters).

## Pinned SDK and implementation evidence

Fig uses `github.com/dio/envoy/source/extensions/dynamic_modules` at
`0a804c57cf5fbd56da553062bebc9b88482b0d1b`. Inspected the installed module and the
C++ implementation at that exact revision, rather than assuming latest documentation
matches the local binary. Relevant sources:

- [HTTP Go interface](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/source/extensions/dynamic_modules/sdk/go/shared/http.go):
  `GetFilterState`, `SetFilterState`, typed variants, `GetData`, `SetData`.
- [Go ABI implementation](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/source/extensions/dynamic_modules/sdk/go/abi/internal.go):
  actual conversions, shared-data registry and stream-complete cleanup.
- [C ABI](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/source/extensions/dynamic_modules/abi/abi.h):
  filter-state bytes, typed objects and opaque objects; buffer ownership.
- [HTTP callbacks](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/source/extensions/filters/http/dynamic_modules/abi_impl.cc):
  byte setter delegates to ContextAccessor; getter reads StringAccessor.
- [ContextAccessor](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/source/extensions/dynamic_modules/abi_context_accessors.cc)
  and [StringAccessorImpl](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/source/common/router/string_accessor_impl.h):
  setter constructs an owning string, so the supplied bytes are copied.
- [FilterState interface](https://github.com/dio/envoy/blob/0a804c57cf5fbd56da553062bebc9b88482b0d1b/envoy/stream_info/filter_state.h):
  default lifespan is FilterChain, default upstream sharing is None. The raw HTTP
  callback supplies no override. Same key with different lifespan is an error.

### Raw bytes: usable, with wrapper requirements

The C byte setter returns bool. The Go setter returns nothing and ignores that
result. The Go getter reports absent for zero-length data. Getter bytes are borrowed;
the ABI guarantees validity through the current event hook unless a setter invalidates
them. Copy before retaining or calling another operation that could replace state.

The ABI comments mention read-only failure, but the inspected C++ setter delegates
to an interface without a read-only parameter and returns true after setData. Do not
claim native write-once enforcement based on that comment. Fig needs its own trusted
single-writer contract, collision detection and read-back verification. These are not
an atomic compare-and-swap facility or isolation from other in-process native code.

The default FilterChain lifetime is narrower than a recreated logical request.
Do not promise survival across internal redirects/recreate_stream, downstream-to-upstream
visibility, or retries from the presence of this API. Those require specific proof.

### Typed objects are a different concept from typed Fig values

`SetFilterStateTyped` finds an Envoy ObjectFactory by key and calls createFromBytes.
A Fig JSON type name does not create such a factory. `GetFilterStateTyped` serializes
an existing native object; it does not return an arbitrary Go object.

The opaque-object C ABI supports an owned object and destructor, including lifespan
selection. No public Go HttpFilterHandle method for this was found in the pinned SDK.
Using it would require a supported binding and an integer-handle/owned-wrapper protocol,
not storing Go pointers in C++ state. This is outside the first slice.

### SetData/GetData needs stronger qualification

Despite an interface comment saying the data is not included in DynamicMetadata
responses, the implementation writes a pointer-shaped lookup token into
`composer.shared_data` metadata. The value itself remains in the Go registry.
`GetData` searches that registry; it does not dereference arbitrary metadata as Go data.

`clearData` removes entries recorded by the producing handle before invoking its
`OnStreamComplete`. Consumers cannot assume lookup remains available through other
filters' completion callbacks or access logging. A separately loaded Go library is
not guaranteed to share this registry or type identity. Neither module path reuse nor
artifact reuse proves a shared runtime registry. Do not use this as Fig's portable
handoff or cross-filter parsed-document contract without additional evidence.

## Existing project patterns

Source snapshots were read from local checkouts; Plum and Citrus have unrelated
uncommitted work, which was left untouched. Base revisions identify context, not a
fresh upstream verification or deployed behavior.

- **Fig:** `apps/waf/envoy/filter.go` and `apps/marker/envoy/filter.go` write outcome
  metadata and retain private results. `hosts/envoy/internal/filter/match.go` parses
  one document for multiple facts inside one filter. No cross-filter typed handoff
  is implemented. Fixed metadata namespaces also lack distinct producer-instance keys.
- **Plum**, base `d5f516e99a5248463ebbfbff810ec86139e2f0e9`:
  `pipeline/match/match.go`, `Decision.Apply`, publishes backend/binding as filter state
  and other decision fields as metadata. `pipeline/adapt/adapt.go`,
  `decisionFromHandoffState`, reconstructs that result. Useful precedent for internal
  handoffs, but not one coherent typed envelope with dependency/generation validation.
- **Citrus Jev**, base `d6966f952d4807c136f5b1c4f413847f7594b6ef`:
  `internal/apps/jev/protocol.go` defines a JSON Request with Model, State and Questions;
  ParseRequest validates both model allowlisting and question structure.
  `module/jev/filter.go` processes HTTP `/v1/systemone`, buffers its request and retains
  parsed state per filter. Model extraction can use Fig's existing HTTP body phases.
  Extracting `/model` does not replace Jev request validation or authorize dispatch.

Graph indexing of Fig failed; source inspection was used. Plum graph lookup did not
locate the inspected pipeline method, so direct source was used as authoritative.

## Recommendation

Start with bounded, nonempty serialized envelopes in raw-byte filter state for
small facts and app results. Keep the contract host-independent and SDK handling
in a reusable Envoy adapter module. Keep parsed Body documents local for now.

The [behavior design](../REQUEST_DATA.md) defines dependencies, publication, ordering,
errors, lifetime and the local qualification matrix. Implementation follows only after
that contract is agreed; this survey alone is not proof of runtime correctness.
