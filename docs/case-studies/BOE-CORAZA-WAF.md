# Case study: BoE Coraza WAF on Fig

Status: source analysis and planning estimate, 2026-10-06. No Fig runtime has been
implemented. Existing tests were inspected, not executed for this study.

## Conclusion

Build a Fig WAF module around Coraza, carrying forward BoE's HTTP integration behavior.
Keep Coraza's engine, SecLang directives and CRS. Adapt configuration selection,
prepared-engine ownership and host callbacks to Fig's contracts. A wholesale WAF
rewrite would discard useful behavior and regression coverage without helping the
configuration model.

BoE is already reasonably modular at the engine boundary. Its host adapter combines
phase handling, buffering, local replies, metrics and transaction ownership in one
stream implementation. That is a meaningful unit to preserve initially. Fig should
make policy selection and lifecycle explicit without trying to express every Coraza
rule as a generic Fig predicate.

Estimate for one engineer familiar with Go and Envoy dynamic modules:

- **10–17 engineer-days** for the WAF adapter on an existing, usable Fig substrate.
- **20–34 engineer-days total**, approximately **4–7 working weeks**, for the first
  Fig/WAF vertical slice, including the currently missing minimal substrate.
- These are planning ranges with medium-low confidence until a native compatibility
  spike resolves callback and buffering behavior. They exclude production rollout,
  broad protocol qualification and the optional extensions listed below.

## Evidence and scope

The inspected BoE checkout is Citrus's pinned
[`f68a0f6dd9984be9b9f2e3342063cb9410411be0`](https://github.com/tetratelabs/built-on-envoy/tree/f68a0f6dd9984be9b9f2e3342063cb9410411be0),
not a claim about the latest upstream version. Citrus was inspected at base
`d6966f9`, including its local build scripts; unrelated local changes were preserved.

| Source | What it establishes |
|---|---|
| [waf.go](https://github.com/tetratelabs/built-on-envoy/blob/f68a0f6dd9984be9b9f2e3342063cb9410411be0/extensions/composer/waf/waf.go) | Factories, route overrides, stream transaction, request/response phases, blocking and cleanup |
| [coraza/config.go](https://github.com/tetratelabs/built-on-envoy/blob/f68a0f6dd9984be9b9f2e3342063cb9410411be0/extensions/composer/waf/coraza/config.go) | JSON directives/mode input and engine preparation |
| [coraza/directives_fs.go](https://github.com/tetratelabs/built-on-envoy/blob/f68a0f6dd9984be9b9f2e3342063cb9410411be0/extensions/composer/waf/coraza/directives_fs.go) | Embedded directives, CRS and host-filesystem fallback |
| [metrics.go](https://github.com/tetratelabs/built-on-envoy/blob/f68a0f6dd9984be9b9f2e3342063cb9410411be0/extensions/composer/waf/metrics.go) | Transaction count, processing duration and block labels |
| [waf_test.go](https://github.com/tetratelabs/built-on-envoy/blob/f68a0f6dd9984be9b9f2e3342063cb9410411be0/extensions/composer/waf/waf_test.go) | Callback regression cases, including partial-body limits and route overrides |
| [Citrus overlay generator](https://github.com/tetrateio/citrus/blob/d6966f9/scripts/generate-module-overlay.py) and [captured files adapter](https://github.com/tetrateio/citrus/blob/d6966f9/module/captured.go.txt) | Engine preparation from captured files, removal of host-filesystem fallback; runtime activation excluded from the current overlay |

The four BoE production files above total 952 lines, including comments; `waf.go`
accounts for 698. This excludes logger helpers, composer infrastructure, dependencies
and rule assets. Size suggests a bounded adapter, but does not measure the difficulty
of preserving its streaming behavior.

## Current behavior that the migration must account for

1. **Prepare configuration before requests.** Global and per-route factories compile
   Coraza engines. The most specific route config overrides the factory config. An
   absent engine yields an empty, pass-through filter. Fig must distinguish intentional
   absence from an unavailable *required* policy.
2. **One transaction spans the client stream.** Request headers create a Coraza
   transaction. Request headers/body and response headers/body run their respective
   phases. Completion runs logging and closes the transaction. This belongs in a
   downstream HTTP module, including its response callbacks; it is not a per-upstream
   attempt module.
3. **Body inspection controls forwarding.** The adapter stops headers and buffers
   body chunks when inspection requires it. Header-only messages and trailers still
   trigger the relevant body phase. Body-access settings and processable content types
   affect whether bytes are inspected.
4. **Partial inspection is intentional.** With `ProcessPartial`, rules inspect the
   configured window and subsequent bytes pass through. Existing tests explicitly
   assert that content outside the window is not blocked. Full inspection and bounded
   partial inspection must be distinguishable in the product contract.
5. **SSE and upgrades have restricted coverage.** Response headers run inspection;
   phase 4 is finalized before streaming and subsequent response bodies bypass it.
   This is not a streaming output guardrail. A Fig module must declare that capability
   limitation, so mandatory full-body protection cannot silently select this path.
6. **Local replies need lifecycle awareness.** A response that reaches the filter
   without a request transaction is passed through. Creating a new response-only CRS
   transaction can produce false positives and replace an earlier error response.
   Preserve this distinction from a protected request missing its policy.
7. **Inspection scope and enforcement mode are different.** BoE defaults to
   `REQUEST_ONLY`, supports `FULL`, and deprecates `RESPONSE_ONLY`. Coraza's detection-only
   rule-engine behavior is separate. Request-only must never be translated into
   detection-only.
8. **Resource accounting crosses the adapter boundary.** BoE can increase Envoy's
   body buffer limit based on buffered and incoming sizes. Preserve callback semantics,
   but explicitly reconcile host limits, Coraza limits, concurrency and partial-body
   behavior. The source alone does not establish a safe deployment memory budget.

## Mapping to Fig

| Responsibility | Reuse | Fig change |
|---|---|---|
| Rule parsing, CRS and inspection | Coraza engine and directives | Versioned policy resource and bounded captured assets |
| Choosing a policy | Existing route override semantics as compatibility input | Serializable header/route-fact extraction and selection producing a policy reference |
| Prepared configuration | Compiled Coraza engine | Immutable prepared view, coherent activation and request retention |
| Mode selection | Coraza enforcement semantics | Mode bound to the exact policy revision; no request-time shared-engine mutation |
| Request/response execution | BoE phase and interruption behavior | Narrow Envoy host adapter and per-stream transaction ownership |
| Blocking and continuation | Existing status/local-reply semantics | Explicit results and host-owned stop/buffer/resume behavior |
| Observability | Existing metrics and rule information | Consistent app/policy/revision context, bounded labels and log policy |
| Delivery | Captured bytes/files; Kona as a transport precedent | Delivery feeds preparation; network transport stays outside WAF callbacks |

Proposed flow, with conceptual names rather than a committed API:

```text
policy bytes + captured rule files --> prepare Coraza engine --+
serializable extraction/selection --> prepare selector -------+--> activate generation
policy-bound enforcement mode -------------------------------+

request headers --> capture generation --> extract/select --> retain prepared policy
                 --> open transaction --> request phases --> response phases
                 --> logging/close --> release retained generation
```

Selection must finish before the first phase that needs the chosen engine. The first
slice uses header/route facts. Selecting this WAF policy from a complete body would
require a separate bounded preinspection design and is excluded.

Keep domain-specific processing inside the WAF module. Facts select the policy;
Coraza evaluates its rules and produces interruptions. Fig supplies composition,
ownership and activation primitives. This also keeps a future LLM or MCP module from
inheriting Coraza-specific transaction concepts.

### What Citrus contributes, and what remains new

Citrus captures rule files into an immutable configuration filesystem and removes
BoE's host-filesystem fallback. Reuse that preparation boundary. Inline, mounted-file
and remote delivery should all resolve into accepted policy content before compilation;
request execution should not fetch arbitrary `Include` files.

Citrus also has policy-bound runtime-mode code/templates. However, the inspected
native overlay generator explicitly excludes runtime activation transport. Their
existence does **not** demonstrate native dynamic WAF activation. Budget integration
and qualification of that path as new work.

A retained engine keeps an admitted transaction internally consistent. It does not
by itself authorize new traffic after policy removal. Admission, temporary delivery
outage, rejected replacement and explicit retirement need distinct behavior. Bound
retained generations and long-lived transactions so old policies cannot accumulate
without limit.

## Delivery plan and estimate

All entries are engineer-days, not elapsed service-level commitments. Upper bounds
allow ordinary integration iteration, not a redesign of the host SDK.

### WAF-specific work, assuming Fig primitives exist

| Work | Days | Exit evidence |
|---|---:|---|
| Native compatibility spike and callback mapping | 2–3 | Selected engine processes request/response; blocking and cleanup exercised in the pinned host |
| Policy preparation, captured files and serializable selection | 2–3 | Bad policy rejected before activation; required missing policy blocks admission |
| Port host adapter and transaction ownership | 3–5 | Phase, buffering, partial-body and local-reply behavior preserved |
| Adapter regression coverage and EG deployment qualification | 2–4 | Real backend traffic, update/lifetime and composition cases pass |
| Operational documentation and observability review | 1–2 | Limits, supported modes, failure behavior and diagnostics documented |
| **Subtotal** | **10–17** | WAF adapter on a functioning Fig runtime |

### Additional work because Fig currently contains design only

| Work | Days | Exit evidence |
|---|---:|---|
| Minimal spec/registry/preparation and placement validation | 3–5 | WAF resources compile; unsupported placement/dependencies rejected |
| Activation, capture/release, retirement and bounded retention | 4–7 | Coherent replacement with in-flight ownership and no new-request bypass |
| Envoy integration, one source adapter and deployment harness | 3–5 | Reproducible native module deployment and one update path |
| **Additional subtotal** | **10–17** | Minimal reusable Fig substrate |
| **First vertical slice total** | **20–34** | WAF plus the substrate it needs |

Use one delivery mechanism initially: accepted embedded configuration plus a file-based
replacement path is sufficient to prove the lifecycle. All remote transports, a new
xDS-like protocol, CRDs and CA automation are outside this estimate. Kona may supply
future transport work, but that reuse still needs an explicit integration estimate.

The first 2–3 days are a decision gate: if the selected SDK cannot provide the required
buffer ownership or callback lifetime semantics, revise the estimate before building
more framework. Preserve BoE's standalone adapter as a behavior reference during the
migration. Introduce small seams as needed rather than first refactoring all composer
infrastructure.

## Acceptance gates for implementation

These are proposed gates, **not results claimed by this source study**:

- Allowed requests reach a real backend; header/body denial returns the intended
  response and the backend receives no denied request. Response-phase denial prevents
  protected response bytes from reaching the client; the backend has already run.
- Header-only messages, trailers, disabled body access, content types, body limits and
  `ProcessPartial` preserve their documented inspection coverage.
- Upgrades/SSE have explicit limited-coverage behavior. A configuration requiring full
  response-body protection is rejected or denies unsupported traffic by policy.
- Required policy unavailable at cold start or explicitly removed never becomes the
  empty pass-through filter. Optional absence is a separate explicit configuration.
- Invalid replacement leaves an authorized last-good generation intact. A request
  already admitted retains its engine/mode across an update; new requests capture the
  replacement. Retirement separately controls new admission.
- Stream reset, local reply and completion close each transaction and release its
  generation exactly once. Native callback ownership and borrowed buffers are checked.
- Earlier-filter local replies preserve their original status without starting an
  uninitialized response-only CRS transaction.
- WAF and LLM adaptation coexist in one deployment: WAF is downstream, provider
  credentials/adaptation are upstream, and retries do not recreate the downstream
  transaction or bypass mandatory inspection.
- Concurrent requests and reloads stay within explicit body, compilation and retained
  generation budgets. Measure memory and latency against the pinned BoE baseline;
  source size is not performance evidence.
- Metrics avoid uncontrolled authority-label cardinality. Audit/error logging follows
  a documented sensitive-data policy; structured labels alone do not sanitize the
  underlying Coraza rule message.

## Exclusions and alternatives

Full SSE token inspection, decoded gRPC message inspection, semantic LLM guardrails,
a generic retry executor, every source transport, production load qualification and
multi-version Envoy support are separate estimates. The first slice does not promise
feature parity across those protocols.

Keeping BoE unchanged beside other modules remains a useful deployment baseline and
has the least migration work. It does not by itself provide Fig's shared selection and
activation semantics. Rewriting Coraza rules in Fig would add a rule language and
compatibility burden; there is no demonstrated need for that. The recommended adapter
approach preserves the engine while making its policy and lifecycle composable.
