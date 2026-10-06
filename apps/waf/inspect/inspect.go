// Package inspect owns preparation and phase-1 inspection. It has no bundle or Envoy dependency.
package inspect

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/corazawaf/coraza/v3"
)

type Rule struct {
	ID     int    `json:"id"`
	Header string `json:"header"`
	Equals string `json:"equals"`
}
type Spec struct {
	Engine     string `json:"engine"`
	Mode       string `json:"mode"`
	Inspection string `json:"inspection"`
	Rules      struct {
		Format string `json:"format"`
		Items  []Rule `json:"items"`
	} `json:"rules"`
}
type Prepared struct {
	engine coraza.WAF
	mode   string
}
type Request struct {
	Method, URI, Protocol, Authority string
	Headers                          [][2]string
}
type Outcome struct {
	Mode, Action string
	Matched      bool
	RuleID       int
}

var headerToken = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
var literalToken = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func Prepare(spec Spec) (*Prepared, error) {
	if spec.Engine != "coraza" || spec.Inspection != "request-headers" || spec.Rules.Format != "header-rules/v1" {
		return nil, fmt.Errorf("unsupported WAF engine, inspection or rule format")
	}
	mode := "On"
	switch spec.Mode {
	case "enforce":
	case "detect":
		mode = "DetectionOnly"
	default:
		return nil, fmt.Errorf("invalid WAF mode")
	}
	if len(spec.Rules.Items) == 0 || len(spec.Rules.Items) > 128 {
		return nil, fmt.Errorf("require 1..128 header rules")
	}
	directives := []string{"SecRuleEngine " + mode}
	ids := map[int]bool{}
	for _, rule := range spec.Rules.Items {
		if rule.ID <= 0 || ids[rule.ID] {
			return nil, fmt.Errorf("invalid or duplicate rule ID")
		}
		ids[rule.ID] = true
		if len(rule.Header) > 128 || !headerToken.MatchString(rule.Header) || !literalToken.MatchString(rule.Equals) {
			return nil, fmt.Errorf("unsupported header name or equality literal")
		}
		directives = append(directives, fmt.Sprintf(`SecRule REQUEST_HEADERS:%s "@streq %s" "id:%d,phase:1,deny,status:403,nolog"`, rule.Header, rule.Equals, rule.ID))
	}
	engine, err := coraza.NewWAF(coraza.NewWAFConfig().WithDirectives(strings.Join(directives, "\n")))
	if err != nil {
		return nil, err
	}
	return &Prepared{engine: engine, mode: spec.Mode}, nil
}
func (p *Prepared) Inspect(req Request) (out Outcome, err error) {
	out = Outcome{Mode: p.mode, Action: "continue"}
	tx := p.engine.NewTransaction()
	defer func() {
		tx.ProcessLogging()
		if closeErr := tx.Close(); closeErr != nil {
			err = closeErr
			out.Action = "error"
		}
	}()
	tx.ProcessURI(req.URI, req.Method, req.Protocol)
	tx.AddRequestHeader("Host", req.Authority)
	for _, header := range req.Headers {
		if !strings.HasPrefix(header[0], ":") {
			tx.AddRequestHeader(header[0], header[1])
		}
	}
	interruption := tx.ProcessRequestHeaders()
	rules := tx.MatchedRules()
	out.Matched = len(rules) > 0
	if len(rules) > 0 {
		out.RuleID = rules[0].Rule().ID()
	}
	if interruption != nil {
		out.Action = "block"
	}
	return out, nil
}
