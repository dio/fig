# Envoy request handoff carrier

This independent module stores the root `handoff` envelope in raw-byte Envoy filter
state. It depends on Fig core and the pinned SDK, with no app or composing host imports.

Call `Publish` or `Read` only inside synchronous stream callbacks. Publication checks
for an existing slot and verifies bytes after writing because the pinned SDK setter
returns no status. Reading checks size before copying a borrowed buffer and decodes
owned scalar values. No native handle or borrowed bytes escape the call.

This is a trusted, serialized, single-writer protocol. The raw getter cannot reliably
distinguish every foreign object/empty value from absence; the `fig.handoff.` namespace
must be reserved by composition. There is no atomic compare-and-swap or protection
against arbitrary native writers. The carrier enforces 4 KiB per envelope; the fixture
composition layer separately limits stage count. FilterChain lifetime gives no promise
across retries, recreated streams or upstream attempts.

See [the contract](../../../docs/REQUEST_DATA.md) and run `go test -race ./...` here.
