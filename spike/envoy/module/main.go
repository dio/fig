package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/dio/fig/match"
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// selection belongs to this demonstration adapter, not the Match core.
type selection struct {
	Ref string `json:"ref"`
}
type config struct {
	Generation   string     `json:"generation"`
	Spec         match.Spec `json:"spec"`
	OutputHeader string     `json:"outputHeader"`
	Path         string     `json:"path,omitempty"`
	MaxBodyBytes int        `json:"maxBodyBytes,omitempty"`
}
type configFactory struct {
	shared.EmptyHttpFilterConfigFactory
}
type factory struct {
	shared.EmptyHttpFilterFactory
	config   config
	prepared *match.Prepared[selection]
}

func init() {
	sdk.RegisterHttpFilterConfigFactories(map[string]shared.HttpFilterConfigFactory{
		"fig-match": &configFactory{}, "fig-waf-app": &wafAppConfigFactory{},
	})
}
func (*configFactory) Create(_ shared.HttpFilterConfigHandle, data []byte) (shared.HttpFilterFactory, error) {
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("configuration exceeds 1 MiB")
	}
	var cfg config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one configuration")
	}
	if cfg.Generation == "" {
		return nil, fmt.Errorf("generation required")
	}
	if cfg.OutputHeader != "x-fig-policy" && cfg.OutputHeader != "x-fig-plan" {
		return nil, fmt.Errorf("unsupported demonstration output header")
	}
	if cfg.Spec.Phase == match.BodyComplete && (cfg.MaxBodyBytes < 1 || cfg.MaxBodyBytes > 1<<20) {
		return nil, fmt.Errorf("body limit must be in 1..1048576")
	}
	for _, fact := range cfg.Spec.Facts {
		if fact.Extractor == "json-pointer/v1" {
			var args struct {
				MaxBytes int `json:"maxBytes"`
			}
			if err := json.Unmarshal(fact.Args, &args); err != nil {
				return nil, err
			}
			if args.MaxBytes != cfg.MaxBodyBytes {
				return nil, fmt.Errorf("extractor and adapter body limits must agree")
			}
		}
	}
	prepared, err := match.Prepare[selection](cfg.Spec, match.Builtins(), func(value selection) error {
		if len(value.Ref) == 0 || len(value.Ref) > 128 {
			return fmt.Errorf("reference must contain 1..128 characters")
		}
		for _, r := range value.Ref {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return fmt.Errorf("invalid reference")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &factory{config: cfg, prepared: prepared}, nil
}
func (f *factory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &filter{factory: f, handle: h, body: []byte{}}
}

type filter struct {
	shared.EmptyHttpFilter
	factory      *factory
	handle       shared.HttpFilterHandle
	evaluation   *match.Evaluation[selection]
	fields       map[string]match.Value
	body         []byte
	active, done bool
}

func (f *filter) OnRequestHeaders(headers shared.HeaderMap, end bool) shared.HeadersStatus {
	cfg := f.factory.config
	// Demonstration handoff only: remove caller values even on skipped paths.
	headers.Remove(cfg.OutputHeader)
	path := strings.SplitN(headers.GetOne(":path").ToString(), "?", 2)[0]
	if cfg.Path != "" && cfg.Path != path {
		return shared.HeadersStatusContinue
	}
	f.active = true
	f.fields = map[string]match.Value{
		"method":    match.Text(headers.GetOne(":method").ToString()),
		"path":      match.Text(path),
		"authority": match.Text(headers.GetOne(":authority").ToString()),
	}
	f.evaluation = f.factory.prepared.Begin(cfg.Generation)
	if cfg.Spec.Phase == match.Headers {
		if f.finish(match.Headers) {
			return shared.HeadersStatusContinue
		}
		return shared.HeadersStatusStop
	}
	values := headers.Get("content-type")
	if len(values) != 1 {
		f.reject(415, "unsupported_content_type")
		return shared.HeadersStatusStop
	}
	media, _, err := mime.ParseMediaType(values[0].ToString())
	if err != nil || media != "application/json" {
		f.reject(415, "unsupported_content_type")
		return shared.HeadersStatusStop
	}
	encodings := headers.Get("content-encoding")
	if len(encodings) > 1 {
		f.reject(415, "unsupported_encoding")
		return shared.HeadersStatusStop
	}
	if len(encodings) == 1 && !strings.EqualFold(encodings[0].ToString(), "identity") {
		f.reject(415, "unsupported_encoding")
		return shared.HeadersStatusStop
	}
	if length := headers.GetOne("content-length").ToString(); length != "" {
		n, err := strconv.ParseUint(length, 10, 64)
		if err != nil {
			f.reject(400, "invalid_content_length")
			return shared.HeadersStatusStop
		}
		if n > uint64(cfg.MaxBodyBytes) {
			f.reject(413, "body_limit")
			return shared.HeadersStatusStop
		}
	}
	if end {
		if f.finish(match.BodyComplete) {
			return shared.HeadersStatusContinue
		}
		return shared.HeadersStatusStop
	}
	f.evaluation.Advance(match.Input{Phase: match.Headers, Fields: f.fields})
	return shared.HeadersStatusStop
}

func (f *filter) OnRequestBody(body shared.BodyBuffer, end bool) shared.BodyStatus {
	if !f.active || f.factory.config.Spec.Phase == match.Headers {
		return shared.BodyStatusContinue
	}
	if f.done {
		return shared.BodyStatusStopNoBuffer
	}
	// The callback supplies newly received bytes, not the accumulated chain buffer.
	// Copy borrowed native bytes before the callback returns; leave originals for forwarding.
	if body.GetSize() > uint64(f.factory.config.MaxBodyBytes-len(f.body)) {
		f.reject(413, "body_limit")
		return shared.BodyStatusStopNoBuffer
	}
	for _, chunk := range body.GetChunks() {
		f.body = append(f.body, chunk.ToUnsafeBytes()...)
	}
	if !end {
		return shared.BodyStatusStopAndBuffer
	}
	if f.finish(match.BodyComplete) {
		return shared.BodyStatusContinue
	}
	return shared.BodyStatusStopNoBuffer
}
func (f *filter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if !f.active || f.factory.config.Spec.Phase == match.Headers {
		return shared.TrailersStatusContinue
	}
	if !f.done && f.finish(match.BodyComplete) {
		return shared.TrailersStatusContinue
	}
	return shared.TrailersStatusStop
}
func (f *filter) finish(phase match.Phase) bool {
	result := f.evaluation.Advance(match.Input{Phase: phase, Fields: f.fields, Body: f.body})
	f.body = nil
	switch result.Status {
	case match.Selected:
		f.done = true
		// Metadata is the host-local handoff; headers make the spike visible at the backend.
		f.handle.SetMetadata("fig.match", f.factory.config.Spec.Name, map[string]any{
			"ref": result.Value.Ref, "rule": result.RuleID, "generation": result.Generation,
		})
		f.handle.SetMetadata("fig.match", f.factory.config.Spec.Name+".ref", result.Value.Ref)
		f.handle.SetMetadata("fig.match", f.factory.config.Spec.Name+".generation", result.Generation)
		f.handle.RequestHeaders().Set(f.factory.config.OutputHeader, result.Value.Ref)
		return true
	case match.NoMatch:
		f.reject(404, "no_match")
	default:
		f.reject(400, "invalid_match_input")
	}
	return false
}
func (f *filter) reject(status uint32, code string) {
	f.done = true
	f.body = nil
	if f.evaluation != nil {
		f.evaluation.Cancel()
	}
	f.handle.SendLocalResponse(
		status,
		[][2]string{{"content-type", "application/json"}},
		[]byte(`{"error":"`+code+`"}`),
		"fig_match_"+code,
	)
}
func (f *filter) OnStreamComplete() {
	if f.evaluation != nil {
		f.evaluation.Cancel()
		f.evaluation = nil
	}
	f.body = nil
	f.fields = nil
}
func main() {}
