package waf

import (
	"github.com/dio/fig/handoff"
	"github.com/dio/fig/match"
)

const OutcomeType = "fig.waf-outcome/v1alpha1"

// OutcomeRecord is the installed export schema. It carries observations, not an
// authorization grant. No raw headers, body or credentials are exported.
func (r Result) OutcomeRecord() handoff.Record {
	return handoff.Record{
		"matched": match.Bool(r.Outcome.Matched),
		"action":  match.Text(r.Outcome.Action),
		"policy":  match.Text(r.Policy),
	}
}
