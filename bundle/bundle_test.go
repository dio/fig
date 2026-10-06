package bundle

import (
	"bytes"
	"testing"
)

func TestDecodeRejectsAmbiguousJSON(t *testing.T) {
	for _, data := range []string{
		`{"apiVersion":"fig/v1alpha1","apiVersion":"fig/v1alpha1"}`,
		`{"apiVersion":"fig/v1alpha1","scope":"s","generation":"1","resources":[],"extra":1}`,
		`null`, `{}`, `{} {}`,
	} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	if _, err := Decode(bytes.Repeat([]byte(" "), MaxBytes+1)); err == nil {
		t.Fatal("oversized input accepted")
	}
}
func TestExactReferenceAndOwnedResources(t *testing.T) {
	b, err := Decode([]byte(`{"apiVersion":"fig/v1alpha1","scope":"s","generation":"1","resources":[{"type":"app/v1","name":"p","version":"1","spec":{"a":1}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{Type: "app/v1", Name: "p", Version: "1"}
	data, err := b.Resolve(ref)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'x'
	again, _ := b.Resolve(ref)
	if again[0] != '{' {
		t.Fatal("resource ownership leaked")
	}
	ref.Version = "2"
	if _, err := b.Resolve(ref); err == nil {
		t.Fatal("wrong revision resolved")
	}
}
