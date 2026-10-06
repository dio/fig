package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/dio/fig/match"
)

type policy struct {
	PolicyRef string `json:"policyRef"`
}
type plan struct {
	PlanRef string `json:"planRef"`
}

func main() {
	early, err := match.Prepare[policy](match.Spec{
		Schema: "fig.match/v1", Name: "edge-waf", Revision: "1", Phase: match.Headers,
		Facts: []match.FactSpec{{Name: "host", Extractor: "input-field/v1", Type: match.String,
			Args: json.RawMessage(`{"name":"hostname"}`)}},
		Rules: []match.Rule{{ID: "api", When: match.Predicate{Op: "equals", Fact: "host", Values: []match.Value{match.Text("api.example.com")}},
			Result: json.RawMessage(`{"policyRef":"api-policy"}`)}},
		Default: json.RawMessage(`{"policyRef":"baseline"}`),
	}, match.Builtins(), func(p policy) error {
		if p.PolicyRef != "api-policy" && p.PolicyRef != "baseline" {
			return fmt.Errorf("unknown policy")
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("early: %+v\n", early.Begin("generation-1").Advance(match.Input{
		Phase: match.Headers, Fields: map[string]match.Value{"hostname": match.Text("api.example.com")},
	}))
	late, err := match.Prepare[plan](match.Spec{
		Schema: "fig.match/v1", Name: "chat-plan", Revision: "1", Phase: match.BodyComplete,
		Facts: []match.FactSpec{{Name: "model", Extractor: "json-pointer/v1", Type: match.String,
			Args: json.RawMessage(`{"pointer":"/model","maxBytes":65536,"maxDepth":32}`)}},
		Rules: []match.Rule{{ID: "support", When: match.Predicate{Op: "equals", Fact: "model", Values: []match.Value{match.Text("support-chat")}},
			Result: json.RawMessage(`{"planRef":"support"}`)}},
	}, match.Builtins(), func(p plan) error {
		if p.PlanRef != "support" {
			return fmt.Errorf("unknown plan")
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	evaluation := late.Begin("generation-1")
	fmt.Printf("headers: %+v\n", evaluation.Advance(match.Input{Phase: match.Headers}))
	fmt.Printf("body: %+v\n", evaluation.Advance(match.Input{
		Phase: match.BodyComplete, Body: []byte(`{"model":"support-chat"}`),
	}))
}
