# Waf Envoy adapter

Independent Go module adapting the Waf app to Envoy HTTP callbacks.
`ConfigFactory` implements the SDK factory interface. The executable chooses the
registration name; importing this package does not register anything globally.

Dependencies are the app logic, Fig core and the pinned Envoy SDK. This package does
not depend on `hosts/envoy` or the other app's adapter. Standard `go.mod`/`go.sum`
files pin the qualified local SDK. When composing adapters, the executable must use
that same SDK/ABI; dependency-module replace directives are not inherited by callers.

Run `go test ./...` here for compilation. The root `make native-test` builds the
composed library and exercises this adapter through one actual local Envoy process.
