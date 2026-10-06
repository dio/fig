package envoy_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	configrender "github.com/dio/fig/spike/envoy/bootstrap"
	"github.com/dio/kona/envoytest"
)

//go:embed envoy.json
var bootstrapTemplate string

func TestNativeMatch(t *testing.T) {
	if os.Getenv("ENVOY_BIN") == "" {
		t.Skip("set ENVOY_BIN and FIG_MODULE to run native Envoy")
	}
	var received atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			http.Error(w, err.Error(), 413)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"policy": r.Header.Get("x-fig-policy"), "plan": r.Header.Get("x-fig-plan"), "body": string(body)})
	}))
	t.Cleanup(backend.Close)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	bundleData, err := os.ReadFile("../../examples/config/waf.json")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := configrender.Render([]byte(bootstrapTemplate), bundleData)
	if err != nil {
		t.Fatal(err)
	}
	config := strings.NewReplacer(
		"/usr/local/lib/libfig_match.so", "{{.Module}}",
		`"address": "0.0.0.0"`, `"address": "127.0.0.1"`,
		`"port_value": 10000`, `"port_value": {{.ProxyPort}}`,
		`"port_value": 9901`, `"port_value": {{.AdminPort}}`,
		`"address": "echo"`, `"address": "127.0.0.1"`,
		`"port_value": 8080`, `"port_value": `+port,
	).Replace(string(rendered))
	process := envoytest.Start(t, envoytest.Options{Module: os.Getenv("FIG_MODULE"), Bootstrap: config, Env: []string{"GODEBUG=cgocheck=0"}})
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	send := func(path, body, contentType, encoding string) (int, map[string]string, error) {
		req, err := http.NewRequest(http.MethodPost, process.URL+path, strings.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("content-type", contentType)
		req.Header.Set("x-fig-policy", "spoofed")
		req.Header.Set("x-fig-plan", "spoofed")
		if encoding != "" {
			req.Header.Set("content-encoding", encoding)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		result := map[string]string{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		return resp.StatusCode, result, err
	}
	t.Run("header-default", func(t *testing.T) {
		status, result, err := send("/headers", "", "application/json", "")
		if err != nil || status != 200 || result["policy"] != "baseline" || result["plan"] != "" {
			t.Fatalf("%d %v %v", status, result, err)
		}
	})
	cases := []struct {
		name, body, contentType, encoding string
		status                            int
	}{
		{name: "selected", body: `{"model":"support-chat"}`, status: 200},
		{name: "unknown", body: `{"model":"other"}`, status: 404},
		{name: "missing", body: `{}`, status: 404},
		{name: "malformed", body: `{`, status: 400},
		{name: "null", body: `{"model":null}`, status: 400},
		{name: "wrong-type", body: `{"model":1}`, status: 400},
		{name: "duplicate", body: `{"model":"support-chat","model":"other"}`, status: 400},
		{name: "depth", body: `{"model":"support-chat","nested":` + strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33) + "}", status: 400},
		{name: "empty", body: "", status: 400},
		{name: "limit", body: strings.Repeat("x", 4097), status: 413},
		{name: "content-type", body: "{}", contentType: "text/plain", status: 415},
		{name: "encoding", body: "{}", encoding: "gzip", status: 415},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media := tc.contentType
			if media == "" {
				media = "application/json"
			}
			before := received.Load()
			status, result, err := send("/chat", tc.body, media, tc.encoding)
			if err != nil || status != tc.status {
				t.Fatalf("%d %v %v", status, result, err)
			}
			expected := before
			if status == 200 {
				expected++
				if result["plan"] != "support" || result["policy"] != "chat-policy" || result["body"] != tc.body {
					t.Fatalf("incorrect handoff: %v", result)
				}
			}
			if received.Load() != expected {
				t.Fatalf("backend receipt: got %d want %d", received.Load(), expected)
			}
		})
	}
	t.Run("trailers", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, process.URL+"/chat", strings.NewReader(`{"model":"support-chat"}`))
		req.ContentLength = -1
		req.Header.Set("content-type", "application/json")
		req.Trailer = http.Header{"X-Finish": []string{"yes"}}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result := map[string]string{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		if err != nil || resp.StatusCode != 200 || result["plan"] != "support" {
			t.Fatalf("%d %v %v", resp.StatusCode, result, err)
		}
	})
	t.Run("waf-selection-and-next-action", func(t *testing.T) {
		cases := []struct {
			name, path, attack, policy, action, matched string
			status                                      int
		}{
			{"clean", "/chat", "", "chat-policy", "continue", "false", 200},
			{"block", "/chat", "attack", "chat-policy", "block", "true", 403},
			{"detect-only", "/observe", "attack", "observe-policy", "continue", "true", 200},
			{"baseline-block", "/headers", "attack", "baseline", "block", "true", 403},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				before := received.Load()
				req, err := http.NewRequest(http.MethodPost, process.URL+tc.path, strings.NewReader(`{"model":"support-chat"}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("content-type", "application/json")
				req.Header.Set("x-fig-attack", tc.attack)
				req.Header.Set("x-fig-policy", "observe-policy")
				req.Header.Set("x-fig-waf-action", "continue")
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != tc.status || resp.Header.Get("x-fig-waf-action") != tc.action ||
					resp.Header.Get("x-fig-waf-matched") != tc.matched || resp.Header.Get("x-fig-waf-policy") != tc.policy {
					t.Fatalf("status=%d headers=%v body=%s", resp.StatusCode, resp.Header, body)
				}
				expected := before
				if tc.status == 200 {
					expected++
				}
				if received.Load() != expected {
					t.Fatalf("backend receipt got %d want %d", received.Load(), expected)
				}
			})
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		before := received.Load()
		var wg sync.WaitGroup
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := `{"model":"support-chat","id":` + strconv.Itoa(i) + `}`
				status, result, err := send("/chat", body, "application/json", "")
				if err != nil || status != 200 || result["body"] != body || result["plan"] != "support" {
					t.Errorf("%d %v %v", status, result, err)
				}
			}()
		}
		wg.Wait()
		if received.Load() != before+40 {
			t.Errorf("concurrent receipt count %d", received.Load()-before)
		}
	})
	info, err := client.Get(process.AdminURL + "/server_info")
	if err != nil {
		t.Fatal(err)
	}
	defer info.Body.Close()
	var version struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(info.Body).Decode(&version); err != nil {
		t.Fatal(err)
	}
	t.Log(fmt.Sprintf("native Envoy %s; backend received=%d", version.Version, received.Load()))
}

// Validation mode loads module configuration too: an unresolved reference must
// fail before a listener can serve, rather than become request-time pass-through.
func TestNativeRejectsInvalidBundle(t *testing.T) {
	if os.Getenv("ENVOY_BIN") == "" {
		t.Skip("set ENVOY_BIN and FIG_MODULE")
	}
	data, err := os.ReadFile("../../examples/config/waf.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, resource := range document["resources"].([]any) {
		r := resource.(map[string]any)
		if r["type"] == "fig.waf-policy/v1alpha1" && r["name"] == "baseline" {
			r["version"] = "wrong-version"
		}
	}
	invalid, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := configrender.Render([]byte(bootstrapTemplate), invalid)
	if err != nil {
		t.Fatal(err)
	}
	config := strings.ReplaceAll(string(rendered), "/usr/local/lib/libfig_match.so", os.Getenv("FIG_MODULE"))
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Getenv("ENVOY_BIN"), "-c", path, "--mode", "validate")
	command.Env = append(os.Environ(), "GODEBUG=cgocheck=0")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("validation timed out: %s", output)
	}
	if err == nil || !strings.Contains(string(output), "unresolved policy reference") {
		t.Fatalf("invalid bundle did not fail during native configuration: %v\n%s", err, output)
	}
}
