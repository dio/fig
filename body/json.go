// Package body provides bounded, immutable JSON views without host or app dependencies.
package body

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Limits struct {
	MaxBytes int `json:"maxBytes"`
	MaxDepth int `json:"maxDepth"`
	MaxNodes int `json:"maxNodes"`
}

var (
	ErrBytes = errors.New("body_limit")
	ErrDepth = errors.New("depth_limit")
	ErrNodes = errors.New("node_limit")
	ErrJSON  = errors.New("invalid_json")
)

func (l Limits) Validate() error {
	if l.MaxBytes < 1 || l.MaxDepth < 1 || l.MaxDepth > 128 || l.MaxNodes < 1 {
		return errors.New("positive limits and depth in 1..128 required")
	}
	return nil
}

// Document owns its data. It exposes no mutable tree or caller-owned bytes.
type Document struct{ root any }

func Parse(data []byte, limits Limits) (*Document, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if len(data) > limits.MaxBytes {
		return nil, ErrBytes
	}
	if !validUnicode(data) {
		return nil, ErrJSON
	}
	return parse(data, limits)
}

// ParseLegacy preserves json-pointer/v1 decoding semantics during migration.
// New consumers should use Parse, which also rejects invalid Unicode.
func ParseLegacy(data []byte, maxBytes, maxDepth int) (*Document, error) {
	limits := Limits{MaxBytes: maxBytes, MaxDepth: maxDepth, MaxNodes: len(data) + 1}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, ErrBytes
	}
	return parse(data, limits)
}
func parse(data []byte, limits Limits) (*Document, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	remaining := limits.MaxNodes
	root, err := readJSON(d, 0, limits.MaxDepth, &remaining)
	if err != nil {
		if errors.Is(err, ErrDepth) || errors.Is(err, ErrNodes) {
			return nil, err
		}
		return nil, ErrJSON
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrJSON
	}
	return &Document{root: root}, nil
}

// Pointer is an immutable, validated JSON pointer.
type Pointer struct{ parts []string }

func PreparePointer(pointer string) (*Pointer, error) {
	parts := []string{}
	if pointer != "" {
		if !strings.HasPrefix(pointer, "/") {
			return nil, errors.New("invalid JSON pointer")
		}
		for _, part := range strings.Split(pointer[1:], "/") {
			for i := 0; i < len(part); i++ {
				if part[i] != '~' {
					continue
				}
				if i+1 == len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return nil, errors.New("invalid pointer escape")
				}
				i++
			}
			parts = append(parts, strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~"))
		}
	}
	return &Pointer{parts: parts}, nil
}

// Scalar contains only immutable values. Kind is string, boolean, integer or invalid.
// A found null/container/non-integer number has Kind invalid, distinct from absence.
type Scalar struct {
	Kind string
	Text string
	Bool bool
	Int  int64
}

func (d *Document) Lookup(p *Pointer) (Scalar, bool) {
	if d == nil || p == nil {
		return Scalar{}, false
	}
	value := d.root
	for _, part := range p.parts {
		switch node := value.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return Scalar{}, false
			}
			value = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || strconv.Itoa(i) != part || i >= len(node) {
				return Scalar{}, false
			}
			value = node[i]
		default:
			return Scalar{}, false
		}
	}
	switch node := value.(type) {
	case string:
		return Scalar{Kind: "string", Text: node}, true
	case bool:
		return Scalar{Kind: "boolean", Bool: node}, true
	case json.Number:
		if n, err := strconv.ParseInt(string(node), 10, 64); err == nil {
			return Scalar{Kind: "integer", Int: n}, true
		}
	}
	return Scalar{Kind: "invalid"}, true
}

// Validate Unicode escapes before encoding/json can replace malformed sequences.
func validUnicode(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xDC00 && n <= 0xDFFF {
			return false
		}
		if n < 0xD800 || n > 0xDBFF {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		i += 6
	}
	return true
}

// readJSON rejects duplicate keys and bounds nesting before descending.
func readJSON(d *json.Decoder, depth, limit int, remaining *int) (any, error) {
	if *remaining == 0 {
		return nil, ErrNodes
	}
	*remaining--
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	if depth >= limit {
		return nil, ErrDepth
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
			value, err := readJSON(d, depth+1, limit, remaining)
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
			value, err := readJSON(d, depth+1, limit, remaining)
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
