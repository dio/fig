package match

import (
	"encoding/json"
	"sync"
)

// Evaluation serializes Advance and Cancel. Installed extractors must be bounded:
// Cancel cannot interrupt an extractor that is already running under this lock.
type Evaluation[T any] struct {
	mu         sync.Mutex
	view       *Prepared[T]
	generation string
	phase      int
	result     Result[T]
	output     json.RawMessage
}

// Begin captures this prepared view. The generation label is supplied by the owner;
// it is provenance, not an authority token or a resource lease.
func (p *Prepared[T]) Begin(generation string) *Evaluation[T] {
	return &Evaluation[T]{view: p, generation: generation, result: Result[T]{Status: Waiting,
		View: p.name, Revision: p.revision, Generation: generation}}
}

func (e *Evaluation[T]) Advance(input Input) Result[T] {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.result.Status != Waiting {
		return e.snapshot()
	}
	rank := input.Phase.rank()
	if rank == 0 || rank < e.phase {
		return e.fail("invalid_phase")
	}
	e.phase = rank
	if rank < e.view.phase.rank() {
		return e.snapshot()
	}
	facts := map[string]Fact{}
	for _, definition := range e.view.facts {
		fact := definition.extract(input)
		switch fact.State {
		case Present:
			if !fact.Value.valid() || fact.Value.Kind != definition.kind {
				return e.fail("invalid_type")
			}
		case Missing:
		case Invalid:
			// Do not expose arbitrary implementation messages or input values.
			return e.fail("invalid_fact")
		default:
			return e.fail("extractor_contract")
		}
		facts[definition.name] = fact
	}
	for _, rule := range e.view.rules {
		if rule.predicate(facts) {
			e.result.Status = Selected
			e.result.RuleID = rule.id
			e.output = rule.result
			return e.snapshot()
		}
	}
	if e.view.fallback != nil {
		e.result.Status = Selected
		e.result.Default = true
		e.output = e.view.fallback
		return e.snapshot()
	}
	e.result.Status = NoMatch
	return e.snapshot()
}

func (e *Evaluation[T]) Cancel() Result[T] {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.result.Status == Waiting {
		e.result.Status = Cancelled
	}
	return e.snapshot()
}

func (e *Evaluation[T]) fail(code string) Result[T] {
	e.result.Status = Failed
	e.result.Code = code
	return e.snapshot()
}

func (e *Evaluation[T]) snapshot() Result[T] {
	result := e.result
	if result.Status == Selected {
		// Each return gets independently owned maps/slices in the application's T.
		if err := json.Unmarshal(e.output, &result.Value); err != nil {
			result.Status = Failed
			result.Code = "output_decode"
		}
	}
	return result
}
