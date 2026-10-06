# Native Envoy Match spike

This runs the Fig `match` package **inside Envoy as a Go dynamic module**, with a
real backend. It follows [dio/kona](https://github.com/dio/kona)'s shared-library build
and pins the same Envoy/SDK pair. It does not need a Kubernetes cluster.

```text
HTTP client
  -> Envoy downstream filter: early Match (path -> policy reference)
  -> Envoy downstream filter: Coraza phase-1 WAF (selected policy -> next action)
  -> Envoy downstream filter: late Match (/chat JSON model -> plan reference)
  -> Envoy router
  -> echo backend
```

The WAF stage now runs a real Coraza phase-1 rule. Plan references remain selections;
there is no provider adaptation or routing-plan executor in this fixture. Both selections are
recorded in host-local dynamic metadata. The adapter also overwrites demonstration
headers `x-fig-policy` / `x-fig-plan` so the backend can report the selected values.
Those headers are diagnostics, not an authorization or production handoff contract.

## Direct native tests (no Docker)

The preferred development loop follows Transit: Go tests create an `httptest.Server`
backend and launch **one native Envoy child process**. The reusable runner is imported
from the published `github.com/dio/kona/envoytest`, pinned in this module's `go.mod`.
No local Kona checkout or absolute machine-specific replacement is required.

On a fresh macOS arm64 checkout:

```sh
git clone https://github.com/dio/fig.git
cd fig
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

Requirements: Go 1.27.1, Xcode Command Line Tools/cgo compiler, and native Envoy
commit `0a804c57cf5f` (1.40.0-dev). `native.mod` pins the matching SDK override; the
regular `go.mod` retains Envoy 1.38 for the existing Docker lane. Newer is supported
here by matching the SDK to the binary, not by bypassing ABI compatibility checks.

If Envoy is not installed, download the macOS 15+ arm64 release (verified SHA-256
from the publisher):

```sh
mkdir -p .bin
curl -fL --retry 3 \
  https://github.com/dio/envoy-builder/releases/download/envoy-0a804c57-macos15/envoy-darwin-arm64 \
  -o .bin/envoy.download
printf '%s  %s\n' \
  7ca52fc807b9dc0c95303b50ed5bab65df945a2ceb4ebc0440b88377d4fcb76d \
  .bin/envoy.download | shasum -a 256 -c - && \
  chmod +x .bin/envoy.download && mv .bin/envoy.download .bin/envoy
make native-test ENVOY_BIN="$PWD/.bin/envoy"
```

Build/test individually:

```sh
cd spike/envoy
make native-build
ENVOY_BIN=/absolute/path/to/envoy FIG_MODULE="$PWD/.bin/libfig_match.so" \
  go test -modfile=native.mod -v -count=1 -timeout=60s .
```

`make native-test` checks the Envoy commit. Direct Go test callers must supply matching
artifacts. The native suite skips when `ENVOY_BIN` is unset; with it set, missing or
incompatible artifacts fail. Temporary configs, backend listeners and the Envoy process
are cleaned up by the test. Build artifacts under `.bin` remain available for reuse.

The native lane uses the same `GODEBUG=cgocheck=0` SDK workaround. It exercises header
and body selection, spoof replacement, default/no-match/error paths, trailers and 40
concurrent requests. The Compose lane below additionally covers deliberately fragmented
bodies, streaming overflow and client aborts. Neither lane qualifies live view updates.

## Run the automated live check

Requires Docker with Linux containers, Docker Compose v2, and Python 3. No host Go
installation is required; Go and the Linux shared library build inside Docker.

From the repository root:

```sh
python3 spike/envoy/verify.py
```

The script builds the images, allocates random loopback ports, waits for readiness,
runs real HTTP requests, and removes its uniquely named containers, network and image
tags in a `finally` block. Docker build cache remains. Existing containers and Kubernetes
contexts are untouched. A forced process kill can prevent cleanup; the Compose project
name appears in the output for explicit recovery.

## Run manually and leave Envoy up

From the repository root:

```sh
docker compose -p fig-match-manual -f spike/envoy/compose.yaml up --build -d
docker compose -p fig-match-manual -f spike/envoy/compose.yaml port proxy 10000
```

Use the printed address (for example `127.0.0.1:49152`):

```sh
curl http://127.0.0.1:49152/headers
curl http://127.0.0.1:49152/chat \
  -H 'content-type: application/json' \
  -d '{"model":"support-chat"}'
curl -i http://127.0.0.1:49152/chat \
  -H 'content-type: application/json' \
  -d '{"model":"unknown"}'
```

The first response reports policy `baseline` and no plan. The second reports
`chat-policy`, plan `support`, and the unchanged request body. The third returns 404
without reaching the backend. `compose port echo 8080` exposes the backend's `/count`
endpoint; reading that counter does not increment it. `compose port proxy 9901`
exposes Envoy admin on loopback.

Cleanup:

```sh
docker compose -p fig-match-manual -f spike/envoy/compose.yaml down --rmi local
```

## Files and functional boundary

- `module/main.go`: registered `fig-match` config factory and HTTP stream adapter.
- `envoy.json`: two filter configurations embedding serializable Match specs.
- `echo/main.go`: backend receipt counter and echo; no Fig logic.
- `Dockerfile`: native shared-library and backend builds, then separate runtime images.
- `verify.py`: real traffic assertions, including absence of backend receipt on denial.

The nested Go module keeps the Envoy SDK/ABI dependency out of the host-independent
Match package. Config creation calls `match.Prepare`. Per-stream headers call `Begin`,
then `Advance`. Header selection continues immediately. Body selection stops headers,
copies bounded callback bytes, asks Envoy to buffer original bytes, and continues only
after successful selection. Trailers also finalize body selection. Stream completion
cancels any pending evaluation and drops owned state.

The adapter holds an owned body copy in addition to Envoy's forwarding buffer. It
checks sizes before copying and rejects above 4096 bytes in this fixture. This is
bounded duplication, not a zero-copy implementation or aggregate memory budget. The
listener buffer limit is 16 KiB; HCM request/idle timeouts are 10 seconds. Production
concurrency limits and shared buffering with WAF remain separate work.

Only `/chat` uses the body-dependent stage. It accepts `application/json` with identity
content encoding. It does not parse compressed bodies or arbitrary JSON media types.
All configured JSON-pointer limits must agree with the adapter's body limit. The early
stage supplies raw method, path-without-query and authority fields; it does not claim
normalized or trusted identity. Reference validation here checks syntax only, not a
real policy/plan catalog.

Static filter configuration prepares each instance independently. Both fixture specs
carry `generation-1`, but that label does not implement coherent multi-view publication.
There is no live update channel, shared generation manager, resource lease or revocation.
Changing `envoy.json` requires rebuilding/restarting this fixture.

## Pins and cgo

- Envoy: `envoyproxy/envoy:v1.38.0`.
- SDK: `v0.0.0-20260423231439-f1dd21b16c24`, matching Envoy's commit.
- Build toolchain: `golang:1.27.1-bookworm`.
- Build: `CGO_ENABLED=1 go build -buildmode=c-shared`.
- Runtime: `GODEBUG=cgocheck=0`, following Kona's workaround for this pinned SDK.

The cgo setting disables the runtime pointer checker; it is a known limitation of
this spike, not evidence that native ownership is production-qualified. `cgocheck=2`
is not a valid modern runtime setting. See
[Kona's certificate/cgo notes](https://github.com/dio/kona/blob/main/docs/certificates.md).
Keep the SDK and Envoy pins together when upgrading. The shared library is built for
the Docker engine's Linux architecture, not the host macOS ABI.

## Observed qualification

The direct native suite also passed on 2026-10-06 on macOS arm64 against
`0a804c57cf5fbd56da553062bebc9b88482b0d1b/1.40.0-dev/Clean/RELEASE/BoringSSL`.
It recorded 43 accepted backend requests and no receipt for denied requests. The
installed binary was used; the documented download recipe was not rerun.

On 2026-10-06, the script completed on Linux arm64 containers on the local Docker
engine. Envoy reported:

```text
f1dd21b16c244bda00edfb5ffce577e12d0d2ec2/1.38.0/Clean/RELEASE/BoringSSL
```

Passed:

- Native module loading, early and late selection, default result, replacement of
  spoofed demonstration headers, and unchanged forwarded bodies.
- Eleven rejection cases: unknown/missing model, malformed JSON, null/wrong type,
  duplicate keys, excessive nesting, empty body, excessive content length,
  unsupported media type and unsupported encoding. All had zero backend receipt.
- Fragmented chunked bodies, completion through trailers, no backend dispatch while
  the body is incomplete, and a streaming overflow without a content-length header.
- Client disconnect before completing input, then successful subsequent traffic.
- Forty concurrent requests with distinct bodies on two Envoy workers, with no observed
  cross-request state leakage. Total accepted backend requests across the run: 45.
- Cleanup of the run's containers, network and image tags.

This is a functional spike, not a race-detector, load, HTTP/2 or memory-leak qualification.
It does not exercise Envoy Gateway/k3d, remote mTLS, SDS, configuration rollover or actual
WAF/LLM execution. Kona remains the reference for those deployment/transport concerns.

## WAF selection and next-action spike

`fig-waf` consumes the early Match policy reference and generation from Envoy-local
metadata. Diagnostic request headers cannot select a less restrictive policy. Engines
are prepared at config creation, and each request gets a separate transaction. This
phase-1-only transaction is finalized and closed within the headers callback.

One deliberately fixed Coraza v3.7.0 rule matches `X-Fig-Attack: attack` (rule 1001).
The policies select `SecRuleEngine On` or `DetectionOnly`:

| Path/input | Policy | Matched | Action | HTTP |
|---|---|---|---|---|
| `/chat`, valid model, no attack header | chat-policy | false | continue | 200 |
| `/chat`, attack header | chat-policy | true | block | 403 |
| `/observe`, attack header | observe-policy | true | continue | 200 |
| `/headers`, attack header | baseline | true | block | 403 |
| `/unconfigured` | unknown-policy | false | error | 503 |

`/unconfigured` is an intentional negative fixture: Match selects a reference absent
from the WAF catalog. The draft bundle compiler should reject this before activation.
Missing selection or a generation mismatch also fails closed at runtime.

Outcomes are stored in `fig.waf` dynamic metadata and shown in response headers
`x-fig-waf-policy`, `x-fig-waf-action`, `x-fig-waf-matched`, and `x-fig-waf-rule`.
A continuing WAF result does not imply that later Match or access checks will allow
the request. Upstream responses cannot overwrite these diagnostic outcome headers.

The expanded `make native-test` passed on macOS arm64 / Envoy `0a804c57` with all
previous Match cases plus these five WAF cases. Exactly 45 requests reached the
backend; WAF blocks and the missing-policy error did not. Spoofed policy/action request
headers did not bypass enforcement. The Docker lane was not rerun for this addition.

Scope: request-header inspection only, a fixed smoke rule, no CRS, body/response
inspection, client-IP rules, live policy replacement, audit-log integration or performance
qualification. This establishes selection -> inspection -> next action, not general
WAF coverage. [Configuration draft](../../docs/CONFIG.md) describes the proposed next
step; the module does not consume that bundle schema yet.
