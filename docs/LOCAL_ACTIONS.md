# Local demo and app actions

Status: implemented first local demo. See the runnable [walkthrough](demo.md).
The design started from Fig `0fa78a6`.
Scope: one native Envoy process, WAF and Marker, local operator control.

## Decision and source

The implementation adds a small foreground `fig serve` supervisor and a generic `action` client.
Apps own action names, input schemas and configuration transformations. The supervisor
owns instance targeting, persisted revisions and activation of the complete chain.
Match and request handoff remain independent of this control surface.

The reference is Citrus's [demo, steps 1–3](https://github.com/tetrateio/citrus/blob/8d32c09058e166a2f840e3a70f6fc655d1365563/docs/demo.md):
start a named local runtime, discover app actions and their schemas, preview a JSON
request, apply it, then check traffic. Its action input supports inline JSON, files
and stdin. Adopt that operator experience. Fig's first scope is local configuration;
managed enrollment, review workflows and remote control are future concerns.

## Dataplane boundary

- WAF `Compile` prepares immutable Match and Coraza policies. `inspect.Prepare`
  translates `enforce` / `detect` into Coraza engine configuration. Changing a mode
  requires preparing a new policy; there is no existing runtime mode override.
- Marker `CompileWithInputs` admits scalar inputs and prepares immutable selection.
- Envoy adapters compile bundles in config factories. Per-stream filters retain
  their factory's prepared state. No live configuration receiver exists.
- The shared host renderer embeds app wrappers in a static bootstrap and validates
  declared handoff order. It is shared by the demo and tests, not a general topology planner.
- Kona's `envoytest` helper supplies test lifecycle; the current Fig entry point is
  a shared library. The new native CLI owns interactive supervision and the action service.

The smallest first activation mechanism is a supervised stop/start of Envoy after
complete candidate validation. Every mutation preview must say `restart-envoy` and
warn of a short interruption and possible termination of in-flight requests.
Stable supervisor identity does not imply stable Envoy PID or zero downtime.

## Demo experience

These commands are implemented. Use two terminals. `make build` builds the CLI
and native module together. The walkthrough has expected output and recovery steps.

Terminal A:

```sh
make build
./bin/fig init --dir /tmp/fig
./bin/fig serve --dir /tmp/fig up --envoy "$HOME/.tetrate/bin/envoy"
```

`fig init` creates an explicit local composition with instances `waf-a` and
`marker-a`, copies canonical bundles, and selects the WAF → Marker handoff example.
It refuses to overwrite existing state. The first preset uses the fixed downstream
chain on all hosts, proxy port 18080, and a loopback demo echo backend. It does not
implement the hostname-placement proposal. `up` stays in the foreground and owns
Envoy, the backend and the local control socket. Ctrl-C stops all three.

Terminal B:

```sh
./bin/fig serve --dir /tmp/fig status
./bin/fig serve --dir /tmp/fig action
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --help
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
```

Expected initial traffic: 403, with no Marker execution. Preview then apply:

```sh
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --input '{"policy":"baseline","mode":"detect"}' --dry-run
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --input '{"policy":"baseline","mode":"detect"}'
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
```

Preview leaves 403 unchanged. Successful application yields 200 and
`x-fig-marker: waf-detected`; a clean request yields `waf-clean`. The WAF still
inspects. These labels are selected from filter-state input, never a caller header.
Then demonstrate independent app control:

```sh
./bin/fig serve --dir /tmp/fig action marker:SetValue --instance marker-a --input '{"match":"select-marker","rule":"detected","value":"review-needed"}' --dry-run
./bin/fig serve --dir /tmp/fig action marker:SetValue --instance marker-a --input '{"match":"select-marker","rule":"detected","value":"review-needed"}'
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --input '{"policy":"baseline","mode":"enforce"}'
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
./bin/fig serve --dir /tmp/fig inspect --json
./bin/fig serve --dir /tmp/fig down
```

Expected: marker becomes `review-needed`, then restoring enforcement returns 403.
The Marker value stays configured but Marker does not execute on that blocked request.
File inputs (`--input @request.json`) and stdin (`--input @-`) have the same semantics.
The CLI prints readiness after activation; traffic commands establish actual behavior.

## Action contract and ownership

| Surface | Owner | Initial contract |
|---|---|---|
| `waf:SetMode` | WAF | Required `policy` name and `mode` enum `enforce` / `detect`; exactly one existing policy |
| `marker:SetValue` | Marker | Required Match resource name, existing rule ID, and bounded literal `value`; changes only its result |
| Action descriptors | Each app | Versioned ID, summary, input JSON schemas, read/mutate effect; no closures serialized |
| Instance selection | Composition | Required `--instance`; instance must install the named action's app |
| Configuration revision | Supervisor | Exact opaque revision for the complete local composition; no latest-value rebinding |
| Inspection/status | Supervisor | Desired/applied revisions, process readiness, activation strategy, last failure and effective app bundles |

All mutation fields are required; absence, null, unknown fields and invalid values
fail. Names resolve within the selected app bundle; no automatic resource creation,
implicit all-policy update, or silent default. Use Fig's existing `enforce` / `detect`
spelling. Marker applies its existing 1–128 character literal validation and requires
the installed Marker output type. It cannot alter predicates, inputs or placement.

An app handler is a pure operation:

```text
current owned bundle + typed action input -> candidate owned bundle + summary
```

It performs strict decoding and uses the existing app compiler to validate the
candidate. It has no socket, filesystem, process, Envoy handle or activation authority.
Bundle generation changes on a real mutation. Changed resource versions and all exact
references to them are updated consistently by the app, without modifying other app
bundles. A no-op retains all identities and causes no restart.

The shared action protocol handles discovery and opaque requests/results. Registration
happens in the executable composition root. App implementations live under
`apps/waf/control` and `apps/marker/control` within their existing modules. The generic
CLI dispatcher and supervisor have no WAF/Marker switch. The executable may import
both handlers; Marker's own module continues to have no Coraza dependency.

A JSON schema supports discovery and help; app decoding and compilation remain the
authority. This control-plane action is separate from WAF's request-time next action
(`continue`, `block`, `error`). Request traffic cannot invoke these operations.

## Transaction and activation semantics

One supervisor serializes mutations and owns a private directory and Unix socket.
Directory mode 0700 and socket mode 0600 define local operator access. No HTTP admin
listener or remote authorization system is introduced. Bound request sizes and timeouts;
reject unknown instances/actions before handler execution.

1. Read the committed composition revision. Check an optional caller `--if-revision`
   precondition. A stale precondition fails; never silently retry against newer state.
2. Transform an owned copy of the target app bundle. Other bundles remain unchanged.
3. Assign one new activation identity; regenerate all wrappers and dependent handoff
   bindings together. A WAF generation change also changes Marker's expected producer
   generation, even though Marker's bundle generation does not change.
4. Compile every affected app with its admitted inputs, validate the complete fixed
   chain, and validate the rendered candidate with the pinned Envoy binary/module.
   Candidate validation uses isolated temporary files and does not bind the live ports.
5. For `--dry-run`, return base revision, semantic diff, affected instances, validation
   result and `activation: restart-envoy`. Delete temporary files; write no durable
   state, signal no live process and mutate no active configuration. A preview reserves
   nothing. Its revision can be supplied unchanged with `--if-revision` on application.
6. For apply, persist immutable candidate files plus a pending operation record before
   stopping old Envoy. Launch one new Envoy with the complete candidate bootstrap;
   only one Envoy serves at a time. Wait for readiness within a bounded deadline.
7. After readiness, atomically commit the active revision pointer and complete the
   operation record. Report `applied` separately from any traffic verification.

A malformed candidate leaves old Envoy running. If replacement startup fails, restart
the previous committed snapshot and report the action as failed with restoration
status. Failed restoration reports unavailable; never claim rollback preserved uptime.
On supervisor crash/restart, terminate or adopt only positively identified owned
children; reconcile pending operations against the committed pointer. Uncommitted
candidates do not become active by directory timestamp. Crash recovery and process
ownership must be implemented before claiming this workflow is resumable.

Repeated setters with the already-applied value are no-ops, making retries after a
lost CLI reply safe for these initial actions. Response output includes operation ID,
base/result revision, whether configuration changed, applied revision, readiness and
last activation error. Discovery/help never mutates state.

## Source files and persistence

`fig init` imports the example files into supervisor-owned revisioned snapshots.
Actions modify those snapshots. They do not rewrite files in the Fig checkout or
silently modify the original import sources. `up` resumes the last committed snapshot.
`inspect` exposes that effective configuration and its provenance.

A later explicit `reload --config @file` replaces the desired composition after the
same admission/activation steps. External file edits alone have no effect. There is
no first-version file watcher, overlay hierarchy or second authoritative mode file.
This makes action-versus-file precedence explicit and avoids losing action state on
restart. A file-authority mode, if added later, needs its own conflict contract.

## Packages and implementation plan

1. Add the minimal shared action descriptor/request/result contract in core; implement
   the two pure app handlers with app-owned schema discovery and compiler validation.
2. Promote the fixed bootstrap renderer into a reusable host package shared by demo
   and native tests. It receives admitted bindings and has no concrete app imports.
3. Add `hosts/envoy/cmd/fig` as the composition root and a small host supervisor/control
   client. Inspect reusable Kona process utilities first; do not build production
   lifecycle around `testing.T` or change the existing test helper contract.
4. Add immutable local state, single-owner socket/process lifecycle, full-chain restart,
   bounded readiness, recovery and revision preconditions.
5. Turn the proposed commands into an executable `docs/demo.md` walkthrough with a
   build target, consumed preset and native end-to-end coverage.

The first implementation supplies these two actions and the fixed chain. Generic bundle
editing, reports, arbitrary app installation, hostname placement and remote delivery
can follow independently. No new dependencies are needed for the design itself.

## Alternatives and risks

| Alternative | Tradeoff |
|---|---|
| Edit JSON and rerun tests | Already possible; lacks an interactive, discoverable app control surface |
| App-specific CLI flags | Small initially; duplicates schemas and couples the CLI to every app |
| Arbitrary JSON Patch action | Flexible; exposes resource internals and weakens app-owned intent/validation |
| Per-app mutable globals or mode overrides | Requires new consistency, lifetime and restart semantics; risks handoff-generation mismatch |
| Whole-listener LDS update | Candidate for later seamless activation; needs pinned native validation of replacement, old streams and complete-chain identity |
| Kona app-data channel | Fits future streaming; needs retained views and coordinated activation across apps |

Restart activation deliberately accepts interruption for the local demo. Full-chain
recompilation may become expensive as policy size grows. Keep the action contract
independent from the activation strategy so later delivery can replace restart
without changing action inputs or putting process control into apps.

## Acceptance gates for implementation

- Discovery is supplied by registered app handlers; wrong instance/app is rejected.
- Dry-run and failed validation preserve files, revision, Envoy PID and traffic behavior.
- Real requests show enforce → detect → enforce, plus Marker-only value changes.
- WAF changes regenerate consumer expectations; no stale-generation 500 after success.
- No-op action preserves revision and PID; two writers with one revision precondition
  cannot both commit. Unrelated app resources and rules remain semantically stable.
- Startup failure, failed restoration, port conflicts, Ctrl-C, disconnects and crashes
  have explicit outcomes; no orphan Envoy or automatic restart of foreign processes.
- Restart resumes committed state, including actions; source edits are inert.
- Invalid/null/unknown fields, missing resources and oversized requests fail before
  live mutation. Existing independent app and native handoff tests keep passing.

## Implementation evidence and limits

The current implementation uses core `action`, app-owned `control` packages,
`hosts/envoy/bootstrap`, `hosts/envoy/internal/local`, and `hosts/envoy/cmd/fig`.
The CLI is the concrete registration root. The pinned Kona helper is tied to
`testing.TB`, so Fig uses a small separate supervisor and retains Kona for tests.
No dependency or module manifest was added.

Native coverage includes discovery, preview isolation, mode/marker changes, exact
revision concurrency, persistence, SIGKILL of the supervisor, replacement failure and
failed restoration. A pipe-watching guardian holds the inherited directory flock
until its Envoy child is reaped. Supervisor restart reads the committed pointer;
it never guesses ownership from a saved PID. A guardian killed independently of its
supervisor is outside this lifecycle guarantee. Startup OS failures and local disk
failures are reported; hardware power-loss durability is not qualified.

Outputs use one shared typed JSON response envelope; descriptors currently advertise
input schemas only. Snapshots preserve unrelated resource content/versions; JSON
whitespace may normalize. Inspection returns both the authored snapshot and regenerated
effective bindings. The initial preset is embedded from the canonical examples; there
is no arbitrary composition import or `reload` command yet. The generation IDs are
content hashes, with no ordering semantics. Disk retention is manual for this demo;
only the latest Envoy launch log is retained. Readiness is established at launch and
status also checks process liveness. A running process is not a traffic verification.

The [walkthrough](demo.md) is the user-facing contract for the implemented commands.
