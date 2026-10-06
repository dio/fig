# Match spike

Experimental Go API; no compatibility promise. Standard library only.

```go
prepared, err := match.Prepare[Selection](spec, match.Builtins(), validateSelection)
// Handle err. Keep prepared in the owner's coherent configuration generation.
evaluation := prepared.Begin("generation-1")
result := evaluation.Advance(input)
// Waiting: provide a later complete input snapshot, or Cancel on stream termination.
// Selected: consume result.Value; policy/plan execution happens elsewhere.
```

Run the WAF-policy and LLM-plan examples:

```sh
go run ./cmd/match-spike
```

## Functions to evaluate

- `Prepare[T]`: validates facts, phase dependencies, predicate types, rule IDs and
  every output/default through a required application validator. It owns retained
  specification bytes and compiled predicates. The validator can check scoped
  references against the owner's captured resource set; it does not rewrite outputs.
- `Begin`: retains that prepared pointer for this evaluation. Replacing the caller's
  active pointer cannot change an evaluation already started.
- `Advance`: accepts a complete phase snapshot, waits until the declared evaluation
  phase, extracts all facts, then applies ordered first-match rules.
- `Cancel`: completes a waiting evaluation. A terminal result cannot change afterward.

`T` is application-owned JSON data: e.g. a policy reference, routing-plan reference
or decision definition. Each selected result is independently decoded, including
nested maps/slices. Do not use custom unmarshaling with side effects or nondeterministic
behavior. The spike has no output registry: the generic type plus validator provides
that seam. Output resource ownership remains the caller's responsibility.

## Implemented subset

- Scalar string/bool/int64 facts; no implicit coercion or normalization.
- `input-field/v1`: reads a named, typed field provided by an in-memory/host adapter.
  It does not parse HTTP or establish trust. Missing keys are missing facts.
- `json-pointer/v1`: complete JSON body, byte/depth limits, duplicate-key rejection,
  object/array traversal and strict scalar types. JSON null at the selected location
  is invalid; absent paths are missing. The host must check content type/encoding and
  enforce the collection limit before allocating the body.
- `equals`, `in`, `exists`, `isMissing`, `all`, `any`, `not`; predicate nesting limited
  to 32. All declared facts are extracted before rules run. Invalid facts fail even
  if an earlier rule would otherwise match. Missing equality is false; its negation
  is true, so require `exists` where needed.
- Explicit default or `NoMatch`; `Waiting`, `Selected`, `Failed`, `Cancelled`.
- Serialized `Advance`/`Cancel` and monotonic phase input. A later snapshot includes
  earlier fields needed by that stage; inputs are not accumulated as deltas.

A custom extractor factory is trusted installed code. It must capture owned arguments,
be bounded, pure and safe for concurrent evaluations. Returning pending at the phase
it promised to support is a contract failure. Cancellation waits for an in-progress
extractor; there are no background goroutines or asynchronous callouts.

## Deliberately absent

No Envoy adapter, HTTP parser, shared buffer broker, source transport, publication
manager, revocation, resource leases, authentication, WAF engine or plan executor.
Generation strings are provenance labels only. Go references retain the prepared
Match view, but external engines/resources still require the eventual runtime's lease
contract. There is no explicit `Release` until that ownership is introduced.

Specs are Go structs with JSON tags. A hardened bounded wire decoder, configuration
size quotas and stable schema are not provided. Built-in JSON extraction currently
parses separately per declared JSON fact; share parsing later if measurements warrant
it. No performance or production-readiness claim is made.

Verification for this initial change: `go build ./...`, `go vet ./...`, and execution
of the example command. No automated test suite was added or run. The acceptance
matrix in [the design](../docs/primitives/MATCH.md) remains work for qualification.
