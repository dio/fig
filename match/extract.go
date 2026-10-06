package match

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Extractors are installed code. Prepare must return a pure, concurrency-safe
// function that owns its arguments and does not retain the supplied Input.
type Extractor func(Input) Fact
type Factory func(FactSpec) (Phase, Extractor, error)
type Registry map[string]Factory

func Builtins() Registry {
	return Registry{"input-field/v1": prepareField, "json-pointer/v1": prepareJSON}
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
	if args.MaxBytes <= 0 || args.MaxDepth <= 0 || args.MaxDepth > 128 {
		return "", nil, fmt.Errorf("positive maxBytes and maxDepth in 1..128 required")
	}
	parts := []string{}
	if args.Pointer != "" {
		if !strings.HasPrefix(args.Pointer, "/") {
			return "", nil, fmt.Errorf("invalid JSON pointer")
		}
		for _, part := range strings.Split(args.Pointer[1:], "/") {
			for i := 0; i < len(part); i++ {
				if part[i] != '~' {
					continue
				}
				if i+1 == len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return "", nil, fmt.Errorf("invalid pointer escape")
				}
				i++
			}
			parts = append(parts, strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~"))
		}
	}
	return BodyComplete, func(in Input) Fact {
		if len(in.Body) > args.MaxBytes {
			return Fact{State: Invalid, Code: "body_limit"}
		}
		d := json.NewDecoder(bytes.NewReader(in.Body))
		d.UseNumber()
		value, err := readJSON(d, 0, args.MaxDepth)
		if err != nil {
			return Fact{State: Invalid, Code: "invalid_json"}
		}
		if _, err = d.Token(); err != io.EOF {
			return Fact{State: Invalid, Code: "invalid_json"}
		}
		for _, part := range parts {
			switch node := value.(type) {
			case map[string]any:
				next, ok := node[part]
				if !ok {
					return Fact{State: Missing}
				}
				value = next
			case []any:
				index, err := strconv.Atoi(part)
				canonical := err == nil && index >= 0 && strconv.Itoa(index) == part
				if !canonical || index >= len(node) {
					return Fact{State: Missing}
				}
				value = node[index]
			default:
				return Fact{State: Missing}
			}
		}
		switch node := value.(type) {
		case string:
			return Fact{State: Present, Value: Text(node)}
		case bool:
			return Fact{State: Present, Value: Bool(node)}
		case json.Number:
			n, err := strconv.ParseInt(string(node), 10, 64)
			if err == nil {
				return Fact{State: Present, Value: Int(n)}
			}
		}
		return Fact{State: Invalid, Code: "invalid_type"}
	}, nil
}

// readJSON rejects duplicate keys and bounds nesting before descending.
func readJSON(d *json.Decoder, depth, limit int) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	if depth >= limit {
		return nil, fmt.Errorf("depth limit")
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("invalid key")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate key")
			}
			value, err := readJSON(d, depth+1, limit)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("invalid object")
		}
		return object, nil
	case '[':
		array := []any{}
		for d.More() {
			value, err := readJSON(d, depth+1, limit)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("invalid array")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter")
	}
}
