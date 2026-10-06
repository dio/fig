package local

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dio/fig/action"
)

func TestSnapshots(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := Snapshot{Port: 18080}
	s.Seal()
	if err := Init(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := load(dir)
	if err != nil || got.Revision != s.Revision {
		t.Fatal(got, err)
	}
	if err := Init(dir, s); err == nil {
		t.Fatal("overwrote existing state")
	}
	next := s.Clone()
	next.Port = 18081
	next.Seal()
	if err := saveSnapshot(dir, next); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(filepath.Join(dir, "pending.json"), map[string]string{"candidate": next.Revision}); err != nil {
		t.Fatal(err)
	}
	got, err = load(dir)
	if err != nil || got.Revision != s.Revision {
		t.Fatal("pending became active", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshots", s.Revision+".json"), []byte(`{"port":18082}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(dir); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "active.json"), []byte(`"../../outside"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(dir); err == nil {
		t.Fatal("unsafe revision accepted")
	}
}
func TestControlAdmission(t *testing.T) {
	sup := &supervisor{options: Options{Registry: Registry{}}, snapshot: Snapshot{Port: 18080}, cancel: func() {}}
	for _, body := range []string{`{"op":"unknown"}`, `{"op":"down","dryRun":true}`, `{"op":"action","dryRun":true}`, `{"op":"action","instance":"unknown","action":"a"}`, `{"op":"status","other":1}`, `{"op":"status","op":"down"}`, strings.Repeat("x", 65537)} {
		req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
		res := httptest.NewRecorder()
		sup.handle(res, req)
		if res.Code != 400 {
			t.Fatal("accepted", body[:min(len(body), 80)], res.Code)
		}
	}
	res := httptest.NewRecorder()
	sup.handle(res, httptest.NewRequest("GET", "/rpc", nil))
	if res.Code != 404 {
		t.Fatal(res.Code)
	}
}
func TestBindingsRejectUnknownAndDuplicate(t *testing.T) {
	reg := Registry{"test": {Prepare: func(action.Config) error { return nil }}}
	for _, s := range []Snapshot{{Port: 0}, {Port: 80, Instances: []Instance{{ID: "a", Factory: "unknown"}}}, {Port: 80, Instances: []Instance{{ID: "a", Factory: "test"}, {ID: "a", Factory: "test"}}}} {
		if _, err := s.Bindings(reg); err == nil {
			t.Fatal("invalid composition accepted")
		}
	}
}
func TestCallMissing(t *testing.T) {
	if _, err := Call(context.Background(), t.TempDir(), Request{Op: "status"}); err == nil {
		t.Fatal("missing supervisor accepted")
	}
}
func TestReadBounded(t *testing.T) {
	if _, err := readBounded(strings.NewReader("1234"), 3); err == nil {
		t.Fatal("oversize accepted")
	}
	data, err := readBounded(strings.NewReader(`{}`), 2)
	if err != nil || !json.Valid(data) {
		t.Fatal(err)
	}
}
