package match

import "testing"

// Keep wire compatibility independent of tests that construct specs using constants.
func TestWireConstants(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{Schema, "fig.match/v1"}, {InputField, "input-field/v1"}, {JSONPointer, "json-pointer/v1"},
		{BodyJSONPointer, "body-json-pointer/v1"},
		{string(Headers), "request-headers"}, {string(BodyComplete), "request-body-complete"},
		{string(String), "string"}, {string(Boolean), "boolean"}, {string(Integer), "integer"},
		{string(Pending), "pending"}, {string(Present), "present"}, {string(Missing), "missing"}, {string(Invalid), "invalid"},
		{string(Waiting), "waiting"}, {string(Selected), "selected"}, {string(NoMatch), "no-match"},
		{string(Failed), "failed"}, {string(Cancelled), "cancelled"},
		{OpAll, "all"}, {OpAny, "any"}, {OpNot, "not"}, {OpEquals, "equals"}, {OpIn, "in"},
		{OpExists, "exists"}, {OpIsMissing, "isMissing"},
		{CodeInvalidPhase, "invalid_phase"}, {CodeInvalidType, "invalid_type"}, {CodeInvalidFact, "invalid_fact"},
		{CodeExtractorContract, "extractor_contract"}, {CodeOutputDecode, "output_decode"},
		{CodeInvalidJSON, "invalid_json"}, {CodeBodyLimit, "body_limit"}, {CodeBodyUnavailable, "body_unavailable"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}
