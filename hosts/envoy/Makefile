ENVOY_BIN ?= $(shell command -v envoy)

.PHONY: native-build native-test
native-build:
	mkdir -p .bin
	CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o .bin/libfig_match.so ./cmd/fig-envoy
native-test: native-build
	@test -n "$(ENVOY_BIN)" || (echo 'Set ENVOY_BIN to the 0a804c57 Envoy binary'; exit 1)
	@"$(ENVOY_BIN)" --version 2>&1 | grep -q 0a804c57 || (echo 'go.mod requires Envoy commit 0a804c57'; exit 1)
	ENVOY_BIN="$(ENVOY_BIN)" FIG_MODULE="$(CURDIR)/.bin/libfig_match.so" go test -v -count=1 -timeout=60s ./integration
