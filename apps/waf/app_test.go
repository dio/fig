package waf

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dio/fig/apps/waf/inspect"
	"github.com/dio/fig/bundle"
)

func example(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../examples/config/waf.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func resource(doc map[string]any, kind string) map[string]any {
	for _, r := range doc["resources"].([]any) {
		value := r.(map[string]any)
		if value["type"] == kind {
			return value
		}
	}
	panic("fixture resource missing")
}
func encode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var entry = bundle.Ref{Type: PipelineType, Name: "edge", Version: "1"}

func TestRejectBeforePreparation(t *testing.T) {
	cases := map[string]func(map[string]any){
		"wrong-policy-version": func(d map[string]any) { resource(d, PolicyType)["version"] = "missing" },
		"wrong-output-type":    func(d map[string]any) { resource(d, MatchType)["spec"].(map[string]any)["outputType"] = "unknown" },
		"body-phase": func(d map[string]any) {
			resource(d, MatchType)["spec"].(map[string]any)["phase"] = "request-body-complete"
		},
		"unavailable-fact": func(d map[string]any) {
			resource(d, MatchType)["spec"].(map[string]any)["facts"].([]any)[0].(map[string]any)["args"] = map[string]any{"name": "principal"}
		},
		"raw-seclang": func(d map[string]any) {
			resource(d, PolicyType)["spec"].(map[string]any)["rules"] = map[string]any{"format": "seclang", "inline": "SecRuleEngine Off"}
		},
		"placement": func(d map[string]any) {
			resource(d, PipelineType)["spec"].(map[string]any)["placement"] = "upstream-http"
		},
		"handoff": func(d map[string]any) {
			resource(d, PipelineType)["spec"].(map[string]any)["steps"].([]any)[1].(map[string]any)["input"] = map[string]any{"from": "missing", "type": SelectionType}
		},
		"fail-open": func(d map[string]any) {
			resource(d, PipelineType)["spec"].(map[string]any)["steps"].([]any)[1].(map[string]any)["onError"] = map[string]any{"localReply": map[string]any{"status": 200}}
		},
		"duplicate-resource": func(d map[string]any) { d["resources"] = append(d["resources"].([]any), resource(d, PolicyType)) },
		"unknown-field":      func(d map[string]any) { resource(d, PolicyType)["spec"].(map[string]any)["unknown"] = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := example(t)
			mutate(doc)
			if prepared, err := Compile(encode(t, doc), entry); err == nil || prepared != nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
}
func TestPolicyComesFromBundleAndPreparedStateIsOwned(t *testing.T) {
	doc := example(t)
	policy := resource(doc, PolicyType)["spec"].(map[string]any)
	rule := policy["rules"].(map[string]any)["items"].([]any)[0].(map[string]any)
	rule["equals"] = "different"
	rule["id"] = 2002
	data := encode(t, doc)
	prepared, err := Compile(data, entry)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = 'x'
	}
	req := inspect.Request{Method: "GET", URI: "/headers", Protocol: "HTTP/1.1", Authority: "example.com", Headers: [][2]string{{"X-Fig-Attack", "attack"}}}
	if result := prepared.Execute(req, "/headers"); result.Status != 0 {
		t.Fatalf("old hardcoded rule still applied: %+v", result)
	}
	req.Headers[0][1] = "different"
	result := prepared.Execute(req, "/headers")
	if result.Status != 403 || result.Outcome.RuleID != 2002 {
		t.Fatalf("configured rule not applied: %+v", result)
	}
}
