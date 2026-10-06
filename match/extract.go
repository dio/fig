package match

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dio/fig/body"
)

// Extractors are installed code. Prepare must return a pure, concurrency-safe
// function that owns its arguments and does not retain the supplied Input.
type Extractor func(Input) Fact
type Factory func(FactSpec) (Phase, Extractor, error)
type Registry map[string]Factory

func Builtins() Registry {
	return Registry{InputField: prepareField, JSONPointer: prepareJSON, BodyJSONPointer: prepareBodyPointer}
}

func decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func prepareField(spec FactSpec) (Phase, Extractor, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := decode(spec.Args, &args); err != nil {
		return "", nil, err
	}
	if args.Name == "" {
		return "", nil, fmt.Errorf("field name is required")
	}
	return Headers, func(in Input) Fact {
		value, ok := in.Fields[args.Name]
		if !ok {
			return Fact{State: Missing}
		}
		return Fact{State: Present, Value: value}
	}, nil
}

func prepareJSON(spec FactSpec) (Phase, Extractor, error) {
	var args struct {
		Pointer  string `json:"pointer"`
		MaxBytes int    `json:"maxBytes"`
		MaxDepth int    `json:"maxDepth"`
	}
	if err := decode(spec.Args, &args); err != nil {
		return "", nil, err
	}
	if args.MaxBytes <= 0 || args.MaxDepth <= 0 || args.MaxDepth > maxJSONDepth {
		return "", nil, fmt.Errorf("positive maxBytes and maxDepth in 1..128 required")
	}
	pointer, err := body.PreparePointer(args.Pointer)
	if err != nil {
		return "", nil, err
	}
	return BodyComplete, func(in Input) Fact {
		doc, err := body.ParseLegacy(in.Body, args.MaxBytes, args.MaxDepth)
		if err != nil {
			code := CodeInvalidJSON
			if err == body.ErrBytes {
				code = CodeBodyLimit
			}
			return Fact{State: Invalid, Code: code}
		}
		return documentFact(doc, pointer)
	}, nil
}

func documentFact(doc *body.Document, pointer *body.Pointer) Fact {
	if doc == nil {
		return Fact{State: Invalid, Code: CodeBodyUnavailable}
	}
	value, found := doc.Lookup(pointer)
	if !found {
		return Fact{State: Missing}
	}
	switch value.Kind {
	case "string":
		return Fact{State: Present, Value: Text(value.Text)}
	case "boolean":
		return Fact{State: Present, Value: Bool(value.Bool)}
	case "integer":
		return Fact{State: Present, Value: Int(value.Int)}
	default:
		return Fact{State: Invalid, Code: CodeInvalidType}
	}
}

// prepareBodyPointer reads an already parsed document; it never parses raw Body.
func prepareBodyPointer(spec FactSpec) (Phase, Extractor, error) {
	var args struct {
		Pointer string `json:"pointer"`
	}
	if err := decode(spec.Args, &args); err != nil {
		return "", nil, err
	}
	pointer, err := body.PreparePointer(args.Pointer)
	if err != nil {
		return "", nil, err
	}
	return BodyComplete, func(in Input) Fact { return documentFact(in.Document, pointer) }, nil
}
