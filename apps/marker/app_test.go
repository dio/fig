package marker

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dio/fig/bundle"
	"github.com/dio/fig/match"
)

var entry = bundle.Ref{Type: PipelineType, Name: "mark", Version: "1"}

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../examples/config/marker.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestMarkerOwnsOutputAndNoMatchSemantics(t *testing.T) {
	data := fixture(t)
	app, err := Compile(data, entry)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = 'x'
	}
	for _, tc := range []struct{ path, value, action string }{{"/chat", "chat-request", "continue"}, {"/observe", "observe-request", "continue"}, {"/other", "", "skip"}} {
		got := app.Execute(map[string]match.Value{"path": match.Text(tc.path)})
		if got.Status != 0 || got.Value != tc.value || got.Action != tc.action {
			t.Fatalf("%s: %+v", tc.path, got)
		}
	}
	got := app.Execute(map[string]match.Value{"path": match.Int(1)})
	if got.Status != 500 || got.Action != "error" {
		t.Fatalf("invalid fact passed: %+v", got)
	}
}
func TestMarkerRejectsInvalidConfiguration(t *testing.T) {
	for _, name := range []string{"waf-output", "unsafe-value", "wrong-version", "handoff", "placement", "unknown-field"} {
		t.Run(name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(fixture(t), &doc); err != nil {
				t.Fatal(err)
			}
			resources := doc["resources"].([]any)
			m := resources[0].(map[string]any)["spec"].(map[string]any)
			chain := resources[1].(map[string]any)["spec"].(map[string]any)
			steps := chain["steps"].([]any)
			switch name {
			case "waf-output":
				m["outputType"] = "fig.waf-policy-ref/v1alpha1"
			case "unsafe-value":
				m["rules"].([]any)[0].(map[string]any)["result"] = map[string]any{"value": "marker\r\nx-injected: true"}
			case "wrong-version":
				steps[0].(map[string]any)["configRef"].(map[string]any)["version"] = "2"
			case "handoff":
				steps[1].(map[string]any)["input"].(map[string]any)["from"] = "missing"
			case "placement":
				chain["placement"] = "upstream-http"
			case "unknown-field":
				chain["policy"] = "waf"
			}
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if p, err := Compile(data, entry); err == nil || p != nil {
				t.Fatal("invalid Marker config accepted")
			}
		})
	}
}
