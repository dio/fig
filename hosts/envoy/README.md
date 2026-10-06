# Local Envoy host

This module owns the Envoy dynamic-module SDK boundary. Current focus is one local
native Envoy process, with no Kubernetes, EG or remote configuration dependency.

- `cmd/fig-envoy`: c-shared entry point; imports the ABI and registers factories.
- `internal/filter`: Envoy callback adapters and the standalone body-Match demo.
- `integration`: actual Envoy traffic tests, bootstrap renderer/template and echo backend.

App compilers live in `apps`, outside this host module. The host imports core and
apps; neither imports this module. Integration helpers are fixture code, not a new
public runtime API. Factory names and configuration remain unchanged by relocation.

From the repository root:

```sh
make native-test ENVOY_BIN="$HOME/.tetrate/bin/envoy"
```

Or run `make native-test` here with the same variable. `native.mod` pins the qualified
macOS arm64 Envoy/SDK combination; default `go.mod` retains the existing 1.38 SDK pin.
Do not mix the built library with a different Envoy ABI. The SDK workaround remains
`GODEBUG=cgocheck=0`, set explicitly by the harness.

The [walkthrough](../../spike/envoy/README.md) contains binary setup and test coverage.
The generated library is `.bin/libfig_match.so` here; the historical name is retained.
There is no new loader, placement engine or configuration transport in this move.
