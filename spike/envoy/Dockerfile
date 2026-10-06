FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY body /src/body
COPY match ./match
COPY matchconfig ./matchconfig
COPY bundle ./bundle
COPY examples/config ./examples/config
COPY apps ./apps
COPY hosts/envoy ./hosts/envoy
WORKDIR /src/hosts/envoy
RUN go mod download
RUN CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o /out/libfig_match.so ./cmd/fig-envoy
RUN CGO_ENABLED=0 go build -trimpath -o /out/fig-echo ./integration/echo
RUN go run ./integration/cmd/bootstrap -out /out/envoy.json
FROM envoyproxy/envoy:v1.38.0 AS proxy
COPY --from=build /out/libfig_match.so /usr/local/lib/libfig_match.so
COPY --from=build /out/envoy.json /etc/envoy/envoy.json
ENV GODEBUG=cgocheck=0
CMD ["envoy", "-c", "/etc/envoy/envoy.json", "--concurrency", "2", "--log-level", "info"]
FROM gcr.io/distroless/static-debian12:nonroot AS echo
COPY --from=build /out/fig-echo /fig-echo
ENTRYPOINT ["/fig-echo"]
