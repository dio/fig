# Hostname placement and app activation

Status: design, not implemented in Fig. The native fixture currently runs one
fixed WAF → Marker → body-Match chain on every host. Its app bundles are consumed;
the placement document below is illustrative and is not accepted by that loader.

## Boundaries

Match is a primitive for extracting facts and selecting typed results. WAF and
Marker are apps: each owns its pipeline, output types, preparation and outcomes.
Placement sits above apps and selects configured app instances for traffic.
An app's internal Match is not responsible for deciding whether that app is attached.

```text
host/controller authority + topology + resolved app bundles
                         |
                 placement planning
                         |
              one shared HTTP listener
                /                   
 a.spike.test: [waf-a, marker-a]   b.spike.test: [waf-b]
                \                   /
                  existing router
```

An artifact is installed code. An instance is a separately configured execution of
an app factory. A placement attaches an ordered list of instances to traffic.
Reusing the artifact or bundle does not merge instances, mutable request state,
credentials or physical filter identities.

## Proposed authoring adapter

```json
{
  "revision": "demo-1",
  "listener": "http",
  "placements": [
    {
      "id": "a",
      "hostname": "a.spike.test",
      "downstream": [
        {"id": "waf", "app": "fig.waf/v1alpha1", "bundle": "waf-config"},
        {"id": "marker", "app": "fig.marker/v1alpha1", "bundle": "marker-config"}
      ]
    },
    {
      "id": "b",
      "hostname": "b.spike.test",
      "downstream": [
        {"id": "waf", "app": "fig.waf/v1alpha1", "bundle": "waf-config"}
      ]
    }
  ]
}
```

`bundle` above is a controller-resolved alias for exact envelope scope, generation
and pipeline entry, not a URL or a request-time latest lookup. The authoring adapter
must resolve every alias before planning. Existing WAF and Marker bundles stay
separate, so each compiler sees only its own resource vocabulary. No app silently
ignores another app's resources. A future combined envelope needs explicit ownership
and dependency-closure validation before dispatch to app compilers.

This is deliberately incomplete as deployment input: the host must also supply
runtime inventory, complete HCM consumer/peer inventory, ownership and independently
resolved required protection. These are controller authority, not optional fields a
request or app author can supply to bypass admission. Unknown aliases, unsupported
placements, conflicting hosts and compilation errors reject the complete candidate.

## Reuse Lime's placement model

Inspected `dio@mini:/Users/dio/src/tetrateio/lime`, clean commit
`1158b1dc89e61c8bd9a4e7b5d4fbd3efd7875ffc` on 2026-10-06. The local workstation's
older Lime tree was not used for this decision. Source evidence in that revision:

- `placement.go`: `Placement`, `Instance`, complete HCM consumers/peers, explicit
  required baselines, ordered chains, composition and logical-to-physical mappings.
- `runtime.go`: artifact, factory and execution-site declarations. `ScopeRef` is
  opaque attribution and grants no authority.
- `build.go`, `Build`: pure admission/composition, stable placement-local filter IDs,
  default-disabled instances and per-host enablement; errors return no partial plan.
- `README.md` and `.agents/skills/lime-integration/SKILL.md`: controller owns rendering,
  policy writes and observation. No EG renderer or upstream planner is implemented.

Lime's current core is a standard-library Go placement SDK, not an EG controller.
Fig should first provide a thin host adapter to its downstream profile, rather than
implement another placement planner. Do not make the Match package depend on Lime.
Keep concrete factory registrations and native/EG rendering in the host adapter.
Pin the dependency when implementing the adapter; this document adds no dependency.

The adapter resolves and validates the bundles, maps their configurations to admitted
factories (`fig-waf-app`, `fig-marker-app`), supplies topology and calls `lime.Build`.
The renderer owns the complete HCM contribution and virtual-host override maps.
It preserves neighboring filters and peer configuration. Independent EG policies
must not be assumed to concatenate into the desired order.

For the example, physical instances are `a/waf`, `a/marker`, `b/waf`, each disabled
by default and enabled only for its host. Physical filter names are distinct from
factory names. A global order can contain these placement-local subsequences without
forcing unrelated placements to share instances or configuration.

## Request semantics

- Initial profile: one shared HCM, exact DNS hostnames, no wildcard placements.
  Duplicates after canonicalization are rejected; overlapping attachments never
  implicitly concatenate. The host adapter must define authority handling: lowercase
  ASCII DNS, optional validated port removal, and one trailing dot normalization.
  Reject malformed authority; do not infer IDNA conversion. Qualify these rules
  against actual Envoy matching before supporting the profile.
- Select once per request using the admitted host attachment. Later path/header
  mutation must not silently reselect a different app chain. Caller-provided app,
  scope and diagnostic headers cannot select instances.
- The order is downstream request order. In `[waf, marker]`, a WAF local reply stops
  processing before Marker. Marker must not report execution on that blocked request.
  Response behavior follows the admitted host callback contract; do not treat a list
  as an arbitrary cross-phase workflow. To mark denied requests, explicitly place a
  suitable observer before WAF and qualify its response behavior.
- An explicit empty optional chain is allowed only after required protection has
  been independently resolved. No-match does not remove mandatory protection.
- The fixture's future unmatched-host policy is an explicit 404. A real controller
  may retain existing peer routes instead; it must inventory those peers and enable
  no Fig placement on them accidentally.
- Authority selection is not authentication. TLS SNI/authority policy, tenant
  authorization and admission remain the host's responsibility. No implicit equality
  policy is introduced here, including for connection reuse.

## Placement compatibility

Apps declare supported execution sites and required phases/capabilities. Current
WAF and Marker support downstream HTTP only. A descriptor must reject an unsupported
site before installation. An LLM app can eventually expose linked downstream Match
and upstream Adapt/credential components, but a downstream list alone cannot express
that linkage. Lime currently rejects upstream intent; keep this refusal until an
explicit cluster-binding model and real upstream execution are qualified. Reusing a
backend address must never merge destination credentials or retry state.

## Publication and lifetime

Distinguish desired placement, prepared app configuration, delivered Envoy config,
observed enabled chain and actual execution. A valid plan proves only the first two
when app compilation was also performed. A blocked update leaves old traffic behavior
in place; it does not close traffic.

The current apps prepare immutable state per filter factory and isolate per-stream
state. There is no atomic generation across independent filters. Initially qualify
static startup. For later Kona delivery, a small filter reference may identify a
separate app data subscription, but publication must retain exact prepared generations
for in-flight requests and clean up failed candidate resources. Cross-app atomic
updates require a shared activation coordinator or a proven host update protocol;
independent watches cannot provide that guarantee. A coordinator is an alternative
for that later requirement, not necessary to prove initial hostname placement.

Deletion, expiry and revocation require explicit new-request admission behavior;
retaining old snapshots for in-flight work does not authorize new traffic. Do not
claim lifecycle safety from a successful initial deployment.

## Implementation and acceptance sequence

1. Compile the two app bundles and admit factories on the pinned native runtime.
2. Add the thin Lime adapter and deterministic native bootstrap renderer; preserve
   peers and required baselines, reject unsupported sites without partial output.
3. Inspect effective Envoy config for distinct disabled instances and exact enablement.
4. Send requests to the same listener with different authorities:

   | Case | Required evidence |
   |---|---|
   | a, clean `/chat` | WAF then Marker; marker reaches backend |
   | b, clean `/chat` | WAF only; no Marker execution |
   | a or b, blocked request | 403, no backend receipt, no Marker execution |
   | unknown host | Explicit 404 in standalone fixture |
   | spoofed diagnostic headers | No change to placement or app decisions |
   | concurrent a/b requests | No configuration or request-state leakage |
   | change only a's config | b's configuration and behavior preserved |
   | invalid bundle/site/baseline | Complete candidate refused; prior state reported |

5. Only then implement an EG renderer at its existing resource owner's assembly point
   and repeat effective-config and traffic assertions. Native proof does not establish
   compatibility with an EG policy surface or a different Envoy/SDK build.

Wildcard precedence, upstream bindings, hot activation and revocation are later
slices. Keep their schemas out of the first exact-host contract until behavior is
implemented and verified.
