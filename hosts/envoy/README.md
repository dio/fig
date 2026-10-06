# Local Envoy host

This module owns the Envoy dynamic-module SDK boundary. Current focus is one local
native Envoy process, with no Kubernetes, EG or remote configuration dependency.

- `cmd/fig-envoy`: c-shared entry point; imports the ABI and registers factories.
- `internal/filter`: standalone body-Match demo, with no concrete app dependencies.
- `integration`: actual Envoy traffic tests, bootstrap renderer/template and in-process test backend.

App compilers live in `apps`, outside this host module. WAF and Marker callbacks
live in their own `apps/<app>/envoy` modules. Only `cmd/fig-envoy` imports and registers
those factories. App adapters and app logic never import this module. Integration helpers are fixture code, not a new
public runtime API. Factory names and configuration remain unchanged by relocation.

From the repository root:

```sh
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

Or run `make native-test` here with the same variable. Standard `go.mod` and `go.sum`
pin the qualified macOS arm64 Envoy/SDK combination (`0a804c57`). No alternate module
manifests or `-modfile` flags are required.
Do not mix the built library with a different Envoy ABI. The SDK workaround remains
`GODEBUG=cgocheck=0`, set explicitly by the harness.

The [walkthrough](../../docs/LOCAL_DEVELOPMENT.md) contains binary setup and test coverage.
The generated library is `.bin/libfig_match.so` here; the historical name is retained.
There is no new loader, placement engine or configuration transport in this move.
