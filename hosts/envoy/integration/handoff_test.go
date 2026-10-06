package envoy_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/handoff"
	configrender "github.com/dio/fig/hosts/envoy/bootstrap"
	"github.com/dio/fig/match"
	"github.com/dio/kona/envoytest"
)

func handoffSlot() handoff.Slot {
	return handoff.Slot{
		Activation: "local-1", Placement: "edge", Producer: "waf-a", Name: "inspection",
		Type: "fig.waf-outcome/v1alpha1", Scope: "demo", Generation: "1",
		Representation: "request-headers@waf-entry", Phase: match.Headers,
	}
}
func handoffBootstrap(t *testing.T) []byte {
	t.Helper()
	waf, err := os.ReadFile("../../../examples/config/waf.json")
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile("../../../examples/config/marker-inspection.json")
	if err != nil {
		t.Fatal(err)
	}
	s := handoffSlot()
	input := handoff.Input{Slot: s, Field: "matched", Fact: "input.inspection", Kind: match.Boolean, Required: true}
	data, err := configrender.Render([]byte(bootstrapTemplate),
		configrender.Binding{Name: "WAF", Entry: bundle.Ref{Type: "fig.pipeline/v1alpha1", Name: "edge", Version: "1"}, Data: waf, Export: &s},
		configrender.Binding{Name: "MARKER", Entry: bundle.Ref{Type: "fig.pipeline/v1alpha1", Name: "mark", Version: "1"}, Data: marker, Input: &input},
	)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Mutate only after normal composition admission, to exercise runtime defenses.
func brokenHandoff(t *testing.T, data []byte, mode string) []byte {
	t.Helper()
	if mode == "normal" {
		return data
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	listener := config["static_resources"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	chain := listener["filter_chains"].([]any)[0].(map[string]any)["filters"].([]any)[0].(map[string]any)["typed_config"].(map[string]any)
	for _, raw := range chain["http_filters"].([]any) {
		f := raw.(map[string]any)
		if f["name"] != "fig-marker-app" && f["name"] != "fig-waf-app" {
			continue
		}
		cfg := f["typed_config"].(map[string]any)["filter_config"].(map[string]any)
		var app map[string]any
		if err := json.Unmarshal([]byte(cfg["value"].(string)), &app); err != nil {
			t.Fatal(err)
		}
		if f["name"] == "fig-waf-app" && (mode == "missing" || mode == "optional") {
			delete(app, "export")
		}
		if f["name"] == "fig-marker-app" {
			input := app["input"].(map[string]any)
			switch mode {
			case "generation":
				input["binding"].(map[string]any)["generation"] = "stale"
			case "type":
				input["binding"].(map[string]any)["type"] = "other/v1"
			case "optional":
				input["required"] = false
			case "field":
				input["field"] = "missing"
			}
		}
		encoded, err := json.Marshal(app)
		if err != nil {
			t.Fatal(err)
		}
		cfg["value"] = string(encoded)
	}
	if mode == "two-producers" || mode == "duplicate" {
		filters := chain["http_filters"].([]any)
		encoded, err := json.Marshal(filters[0])
		if err != nil {
			t.Fatal(err)
		}
		var second map[string]any
		if err := json.Unmarshal(encoded, &second); err != nil {
			t.Fatal(err)
		}
		second["name"] = "fig-waf-second"
		cfg := second["typed_config"].(map[string]any)["filter_config"].(map[string]any)
		var app map[string]any
		if err := json.Unmarshal([]byte(cfg["value"].(string)), &app); err != nil {
			t.Fatal(err)
		}
		if mode == "two-producers" {
			app["export"].(map[string]any)["producer"] = "waf-b"
		}
		// Second inspector matches a different header: its matched result is false.
		for _, r := range app["bundle"].(map[string]any)["resources"].([]any) {
			resource := r.(map[string]any)
			if resource["type"] == "fig.waf-policy/v1alpha1" {
				items := resource["spec"].(map[string]any)["rules"].(map[string]any)["items"].([]any)
				for _, item := range items {
					item.(map[string]any)["header"] = "x-other-attack"
				}
			}
		}
		encoded, err = json.Marshal(app)
		if err != nil {
			t.Fatal(err)
		}
		cfg["value"] = string(encoded)
		ordered := []any{filters[0], second}
		ordered = append(ordered, filters[1:]...)
		chain["http_filters"] = ordered
	}
	result, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestNativeHandoff(t *testing.T) {
	if os.Getenv("ENVOY_BIN") == "" {
		t.Skip("set ENVOY_BIN and FIG_MODULE")
	}
	for _, mode := range []string{"normal", "missing", "generation", "type", "field", "optional", "two-producers", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			var received atomic.Int64
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				_, _ = io.WriteString(w, r.Header.Get("x-fig-marker"))
			}))
			defer backend.Close()
			_, port, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
			raw := brokenHandoff(t, handoffBootstrap(t), mode)
			// Marshal in brokenHandoff may remove whitespace: use a normalized template first.
			var pretty any
			if err := json.Unmarshal(raw, &pretty); err != nil {
				t.Fatal(err)
			}
			raw, err := json.MarshalIndent(pretty, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			config := strings.NewReplacer("__FIG_MODULE__", "{{.Module}}",
				`"address": "0.0.0.0"`, `"address": "127.0.0.1"`,
				`"port_value": 10000`, `"port_value": {{.ProxyPort}}`,
				`"port_value": 9901`, `"port_value": {{.AdminPort}}`,
				`"address": "__FIG_BACKEND__"`, `"address": "127.0.0.1"`,
				`"port_value": 8080`, `"port_value": `+port).Replace(string(raw))
			process := envoytest.Start(t, envoytest.Options{Module: os.Getenv("FIG_MODULE"), Bootstrap: config, Env: []string{"GODEBUG=cgocheck=0"}})
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			send := func(path string, attack bool, want int, marker string) {
				req, err := http.NewRequest(http.MethodGet, process.URL+path, nil)
				if err != nil {
					t.Error(err)
					return
				}
				req.Header.Set("x-fig-marker", "spoofed")
				req.Header.Set("x-fig-waf-matched", "false")
				if attack {
					req.Header.Set("x-fig-attack", "attack")
				}
				response, err := client.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if response.StatusCode != want {
					t.Errorf("status %d want %d: %s", response.StatusCode, want, data)
				}
				if want == 200 && string(data) != marker {
					t.Errorf("marker %q want %q", data, marker)
				}
				if want == 403 && response.Header.Get("x-fig-marker") != "" {
					t.Error("Marker executed after block")
				}
			}
			if mode == "normal" {
				send("/observe", false, 200, "waf-clean")
				send("/observe", true, 200, "waf-detected")
				before := received.Load()
				send("/headers", true, 403, "")
				if received.Load() != before {
					t.Fatal("blocked request reached backend")
				}
				var wg sync.WaitGroup
				for i := range 40 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						value := "waf-clean"
						if i%2 == 1 {
							value = "waf-detected"
						}
						send("/observe", i%2 == 1, 200, value)
					}()
				}
				wg.Wait()
				if received.Load() != 42 {
					t.Fatal("unexpected backend count", received.Load())
				}
			} else if mode == "two-producers" {
				send("/observe", true, 200, "waf-detected")
				if received.Load() != 1 {
					t.Fatal("unexpected count")
				}
			} else if mode == "optional" {
				send("/observe", false, 200, "")
			} else {
				send("/observe", false, 500, "")
				if received.Load() != 0 {
					t.Fatal("failed handoff reached backend")
				}
			}
		})
	}
}
