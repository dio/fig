package match

// Schema identifies the primitive specification, independently of bundle resource types.
const Schema = "fig.match/v1"

// Installed extractor identifiers are serialized in FactSpec.
const (
	InputField      = "input-field/v1"
	JSONPointer     = "json-pointer/v1"
	BodyJSONPointer = "body-json-pointer/v1"
)

// Predicate operators are serialized in Predicate.Op.
const (
	OpAll       = "all"
	OpAny       = "any"
	OpNot       = "not"
	OpEquals    = "equals"
	OpIn        = "in"
	OpExists    = "exists"
	OpIsMissing = "isMissing"
)

// Diagnostic codes contain no raw input or extractor-provided messages.
const (
	CodeInvalidPhase      = "invalid_phase"
	CodeInvalidType       = "invalid_type"
	CodeInvalidFact       = "invalid_fact"
	CodeExtractorContract = "extractor_contract"
	CodeOutputDecode      = "output_decode"
	CodeInvalidJSON       = "invalid_json"
	CodeBodyLimit         = "body_limit"
	CodeBodyUnavailable   = "body_unavailable"
)

const maxPredicateDepth = 32
const maxJSONDepth = 128

const (
	Headers      Phase = "request-headers"
	BodyComplete Phase = "request-body-complete"
)

const (
	String  Kind = "string"
	Boolean Kind = "boolean"
	Integer Kind = "integer"
)

const (
	Pending FactState = "pending"
	Present FactState = "present"
	Missing FactState = "missing"
	Invalid FactState = "invalid"
)

const (
	Waiting   Status = "waiting"
	Selected  Status = "selected"
	NoMatch   Status = "no-match"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)
