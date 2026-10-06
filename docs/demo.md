# Demo: configure apps on one local Envoy

Run WAF → Marker locally, discover their actions, preview a change, and see it affect
real requests. Configuration actions preserve the app boundaries: WAF owns its mode,
Marker owns its selected value, and the supervisor activates their complete chain.

## Prerequisites and build

Use Go 1.27.1, a C compiler, and the matching native Envoy `0a804c57` / 1.40.0-dev.
The [local development guide](LOCAL_DEVELOPMENT.md#native-development-loop-no-docker)
includes the macOS arm64 binary download. Run from the Fig repository root.
No sibling checkout or container runtime is required.

```sh
make build
./bin/fig init --dir /tmp/fig
```

Choose a fresh directory: initialization refuses to overwrite an existing instance.
The default proxy port is 18080; choose another with `fig init --port 18081`.
The directory is private and contains imported, revisioned configuration snapshots.
Changes to repository examples do not change an initialized instance.

## 1. Start — terminal A

```sh
./bin/fig serve --dir /tmp/fig up --envoy "$HOME/.tetrate/bin/envoy"
```

Leave this command running. It starts an echo backend, one Envoy, and a private Unix
control socket. The matching shared library is found beside `bin/fig`; `--module`
can override its path. Startup prints readiness, proxy URL, revision and Envoy PID.
The inherited SDK workaround is explicitly `GODEBUG=cgocheck=0` for Envoy.

## 2. Discover and inspect — terminal B

```sh
./bin/fig serve --dir /tmp/fig status
./bin/fig serve --dir /tmp/fig action
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --help
./bin/fig serve --dir /tmp/fig inspect --json
```

The running app advertises the action's input schema. `inspect` includes the stored
snapshot and effective producer/consumer bindings. Output is JSON by default;
`--json` is accepted explicitly for scripts. Action instance IDs are `waf-a` and
`marker-a`; these are configured instances, not app type names.

Check baseline traffic:

```sh
curl -i http://127.0.0.1:18080/headers
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
```

Expected: **200** with `x-fig-marker: waf-clean`, then **403** with no Marker header.
The enforcing WAF stops the request before Marker or the backend executes.

## 3. Preview WAF mode, then apply

```sh
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --input '{"policy":"baseline","mode":"detect"}' --dry-run
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
```

The preview validates the complete candidate with the real app compilers and native
Envoy. It reports the base/result revisions, semantic summary, affected instances and
restart warning. It writes no durable configuration and leaves the current Envoy PID
unchanged. Traffic still returns **403**.

Apply the same input:

```sh
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --input '{"policy":"baseline","mode":"detect"}'
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
```

Expected: **200**, `x-fig-waf-matched: true`, and `x-fig-marker: waf-detected`.
WAF still inspects. Marker reads WAF's typed result from filter state and uses it
as a Match input. Client diagnostic headers cannot replace that value.

**Applying a changed configuration restarts Envoy.** There is a brief interruption;
in-flight requests may terminate. The supervisor and backend stay running. WAF's new
generation and Marker's expected generation activate together in the new process.
Repeating the same setter is a no-op and preserves the revision and PID.

To bind an apply to a reviewed state, copy `result.baseRevision` from a preview and
supply it as `--if-revision REVISION` on application. A stale revision rejects the
request; it never silently updates against newer state.

## 4. Configure Marker independently

```sh
./bin/fig serve --dir /tmp/fig action marker:SetValue --instance marker-a --help
./bin/fig serve --dir /tmp/fig action marker:SetValue --instance marker-a --input '{"match":"select-marker","rule":"detected","value":"review-needed"}' --dry-run
./bin/fig serve --dir /tmp/fig action marker:SetValue --instance marker-a --input '{"match":"select-marker","rule":"detected","value":"review-needed"}'
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
curl -i http://127.0.0.1:18080/headers
```

Expected: **200** with `review-needed` for detection; a clean request still selects
`waf-clean`. WAF policy content is unchanged. Marker changes only the chosen rule's
literal result; it cannot change the WAF policy or the handoff binding.

All actions accept inline JSON, `--input @request.json`, or `--input @-` from stdin.
Unknown fields, null required values, missing resources and invalid values reject
before changing the running process. For example, `mode: "off"` is unsupported.

## 5. Restore enforcement and resume later

```sh
./bin/fig serve --dir /tmp/fig action waf:SetMode --instance waf-a --input '{"policy":"baseline","mode":"enforce"}'
curl -i -H 'X-Fig-Attack: attack' http://127.0.0.1:18080/headers
./bin/fig serve --dir /tmp/fig down
```

Expected: **403**. Marker keeps its configuration but does not execute on the blocked
request. `down` waits for the owned Envoy to stop. Terminal A returns to its prompt.
Ctrl-C in terminal A also stops the demo. Configuration files remain for resumption:

```sh
./bin/fig serve --dir /tmp/fig up --envoy "$HOME/.tetrate/bin/envoy"
```

The committed snapshot is restored, including both actions. A second supervisor for
the same directory is rejected. If the supervisor crashes, its guardian stops/reaps
Envoy before releasing ownership; restart resumes the committed snapshot and discards
pending activation intent. No process is killed by looking up a saved PID.

A replacement failure returns an error and attempts to restore the committed snapshot.
`status` reports readiness and the activation/restoration error. If restoration also
fails, traffic is unavailable; `down` remains available. Check `envoy.log` in the demo
directory. The log contains only the latest launch; snapshots persist until you remove
the stopped instance directory. This local control surface trusts the owning OS user.

## Verification and current limits

```sh
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

`TestNativeServe` exercises CLI discovery/help, preview isolation, inline/file/stdin
input, mode and marker changes, no-op behavior, stale/concurrent revision checks,
restart persistence, supervisor crash cleanup, failed replacement and failed
restoration. Existing handoff and body-Match tests run alongside it.

This demo uses the current fixed chain on all hosts. `/observe` retains its separately
configured detection policy; the mode action above targets `baseline` only. `/chat`
also runs the existing JSON body-Match example. No hostname placement, live config
stream, reports API, file watcher or seamless reload is implemented. The local workflow
is qualified on the pinned macOS arm64 Envoy; other hosts need their own qualification.

See [local action architecture](LOCAL_ACTIONS.md) for extension points. Future apps
can register their own schemas and pure transformations without adding app-specific
CLI flags or teaching Match about control-plane actions.
