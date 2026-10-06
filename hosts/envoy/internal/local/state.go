// Package local owns the native runtime lifecycle and generic action transport.
package local

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/dio/fig/action"
	"github.com/dio/fig/bundle"
	"github.com/dio/fig/handoff"
	"github.com/dio/fig/hosts/envoy/bootstrap"
)

type App struct {
	Actions []action.Handler
	Prepare func(action.Config) error
}
type Registry map[string]App

type Instance struct {
	ID      string         `json:"id"`
	Name    string         `json:"bindingName"`
	Factory string         `json:"factory"`
	Config  action.Config  `json:"config"`
	Export  *handoff.Slot  `json:"export,omitempty"`
	Input   *handoff.Input `json:"input,omitempty"`
}
type Snapshot struct {
	Revision  string     `json:"revision"`
	Port      int        `json:"port"`
	Instances []Instance `json:"instances"`
}

func (s Snapshot) Clone() Snapshot {
	data, _ := json.Marshal(s)
	var c Snapshot
	_ = json.Unmarshal(data, &c)
	return c
}
func (s *Snapshot) Seal() {
	s.Revision = ""
	data, _ := json.Marshal(s)
	s.Revision = action.Digest(data)
}
func (s Snapshot) Bindings(reg Registry) ([]bootstrap.Binding, error) {
	if s.Port < 1 || s.Port > 65535 {
		return nil, fmt.Errorf("invalid proxy port")
	}
	exports := map[string]handoff.Slot{}
	ids := map[string]bool{}
	for _, i := range s.Instances {
		if i.ID == "" || ids[i.ID] {
			return nil, fmt.Errorf("duplicate or empty instance")
		}
		ids[i.ID] = true
		if _, ok := reg[i.Factory]; !ok {
			return nil, fmt.Errorf("unknown factory %q", i.Factory)
		}
		if i.Export != nil {
			b, err := bundle.Decode(i.Config.Bundle)
			if err != nil {
				return nil, err
			}
			slot := *i.Export
			slot.Activation = s.Revision
			slot.Producer = i.ID
			slot.Scope = b.Scope()
			slot.Generation = b.Generation()
			exports[i.ID] = slot
		}
	}
	bindings := []bootstrap.Binding{}
	for _, i := range s.Instances {
		binding := bootstrap.Binding{Name: i.Name, Entry: i.Config.Entry, Data: i.Config.Bundle}
		if e, ok := exports[i.ID]; ok {
			binding.Export = &e
		}
		if i.Input != nil {
			in := *i.Input
			e, ok := exports[in.Slot.Producer]
			if !ok {
				return nil, fmt.Errorf("missing declared producer")
			}
			// Structural port expectations remain owned by composition; only identity advances.
			expected := in.Slot
			expected.Activation = e.Activation
			expected.Scope = e.Scope
			expected.Generation = e.Generation
			if expected != e {
				return nil, fmt.Errorf("incompatible handoff binding")
			}
			in.Slot = e
			binding.Input = &in
			if i.Config.Inputs[in.Fact] != in.Kind {
				return nil, fmt.Errorf("input type not admitted")
			}
		}
		if err := reg[i.Factory].Prepare(i.Config); err != nil {
			return nil, fmt.Errorf("%s: %w", i.ID, err)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}
func Init(dir string, s Snapshot) error {
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(dir, "snapshots"), 0700); err != nil {
		return err
	}
	s.Seal()
	if err := saveSnapshot(dir, s); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(dir, "active.json"), s.Revision)
}

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func load(dir string) (Snapshot, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return Snapshot{}, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return Snapshot{}, fmt.Errorf("state directory must be private (0700), not a symlink")
	}
	data, err := os.ReadFile(filepath.Join(dir, "active.json"))
	if err != nil {
		return Snapshot{}, err
	}
	var revision string
	if err := json.Unmarshal(data, &revision); err != nil {
		return Snapshot{}, err
	}
	if !revisionPattern.MatchString(revision) {
		return Snapshot{}, fmt.Errorf("invalid active revision")
	}
	f, err := os.Open(filepath.Join(dir, "snapshots", revision+".json"))
	if err != nil {
		return Snapshot{}, err
	}
	defer f.Close()
	data, err = readBounded(f, 4*bundle.MaxBytes)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, err
	}
	s.Seal()
	if s.Revision != revision {
		return Snapshot{}, fmt.Errorf("snapshot digest mismatch")
	}
	return s, nil
}
func saveSnapshot(dir string, s Snapshot) error {
	path := filepath.Join(dir, "snapshots", s.Revision+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err == nil {
		if string(old) != string(data) {
			return fmt.Errorf("snapshot collision")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	// The directory owner holds the exclusive flock (or creates a fresh directory).
	// Publish a complete file so a crash cannot leave a partial immutable snapshot.
	return atomicJSON(path, s)
}
func atomicJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
