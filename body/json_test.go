package body

import (
	"errors"
	"sync"
	"testing"
)

func TestParse(t *testing.T) {
	limits := Limits{MaxBytes: 256, MaxDepth: 2, MaxNodes: 4}
	for _, tc := range []struct {
		name, input string
		want        error
	}{
		{"valid", `{"a":[1,true]}`, nil},
		{"depth", `[[[]]]`, ErrDepth},
		{"nodes", `[1,2,3,4]`, ErrNodes},
		{"duplicate", `{"a":1,"\u0061":2}`, ErrJSON},
		{"empty", "", ErrJSON},
		{"trailing", "{} {}", ErrJSON},
		{"high", `"\ud800"`, ErrJSON},
		{"low", `"\udc00"`, ErrJSON},
		{"pair", `"\ud83d\ude00"`, nil},
		{"literal-escape", `"\\ud800"`, nil},
		{"utf8", string([]byte{'"', 255, '"'}), ErrJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse([]byte(tc.input), limits)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if err != nil && doc != nil {
				t.Fatal("partial document")
			}
		})
	}
	for _, size := range []int{3, 4} {
		_, err := Parse([]byte("null"), Limits{MaxBytes: size, MaxDepth: 1, MaxNodes: 1})
		if (size == 3) != errors.Is(err, ErrBytes) {
			t.Fatalf("byte boundary %d: %v", size, err)
		}
	}
}
func TestLookupOwnershipAndConcurrency(t *testing.T) {
	raw := []byte(`{"a/b":{"~":[null,9223372036854775807,1.0,true,"ok"]}}`)
	doc, err := Parse(raw, Limits{MaxBytes: 128, MaxDepth: 3, MaxNodes: 8})
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = 'x'
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, tc := range []struct {
				path, kind string
				found      bool
			}{
				{"/a~1b/~0/0", "invalid", true}, {"/a~1b/~0/1", "integer", true},
				{"/a~1b/~0/2", "invalid", true}, {"/a~1b/~0/3", "boolean", true},
				{"/a~1b/~0/4", "string", true}, {"/a~1b/~0/01", "", false}, {"/absent", "", false},
			} {
				p, err := PreparePointer(tc.path)
				if err != nil {
					t.Error(err)
					return
				}
				v, found := doc.Lookup(p)
				if found != tc.found || v.Kind != tc.kind {
					t.Errorf("%s: %+v %v", tc.path, v, found)
				}
			}
		}()
	}
	wg.Wait()
	for _, bad := range []string{"a", "/~", "/~2"} {
		if _, err := PreparePointer(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
func TestLegacyUnicode(t *testing.T) {
	if _, err := ParseLegacy([]byte(`"\ud800"`), 100, 2); err != nil {
		t.Fatal(err)
	}
}
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"a":1}`))
	f.Add([]byte(`"\ud800"`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		doc, err := Parse(data, Limits{MaxBytes: 4096, MaxDepth: 32, MaxNodes: 256})
		if err != nil && doc != nil {
			t.Fatal("partial document")
		}
	})
}

func TestIntegerAndLimits(t *testing.T) {
	p, err := PreparePointer("")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ input, kind string }{
		{"9223372036854775807", "integer"}, {"-9223372036854775808", "integer"},
		{"9223372036854775808", "invalid"}, {"1e0", "invalid"}, {"null", "invalid"},
	} {
		doc, err := Parse([]byte(tc.input), Limits{MaxBytes: 64, MaxDepth: 1, MaxNodes: 1})
		if err != nil {
			t.Fatal(err)
		}
		value, found := doc.Lookup(p)
		if !found || value.Kind != tc.kind {
			t.Fatalf("%s: %+v", tc.input, value)
		}
	}
	for _, limits := range []Limits{{}, {MaxBytes: 1, MaxDepth: 129, MaxNodes: 1}} {
		if doc, err := Parse([]byte("0"), limits); err == nil || doc != nil {
			t.Fatal("invalid limits accepted")
		}
	}
}
