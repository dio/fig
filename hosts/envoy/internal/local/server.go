package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/dio/fig/action"
	"github.com/dio/fig/bundle"
)

type Request struct {
	Op         string          `json:"op"`
	Action     string          `json:"action,omitempty"`
	Instance   string          `json:"instance,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	DryRun     bool            `json:"dryRun,omitempty"`
	IfRevision string          `json:"ifRevision,omitempty"`
	Help       bool            `json:"help,omitempty"`
}
type Response struct {
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}
type Status struct {
	Revision   string `json:"appliedRevision"`
	Desired    string `json:"desiredRevision"`
	Ready      bool   `json:"ready"`
	PID        int    `json:"envoyPID,omitempty"`
	URL        string `json:"url"`
	Activation string `json:"activation"`
	LastError  string `json:"lastError,omitempty"`
}
type ActionResult struct {
	Operation string   `json:"operationId"`
	Base      string   `json:"baseRevision"`
	Revision  string   `json:"resultRevision"`
	Changed   bool     `json:"changed"`
	DryRun    bool     `json:"dryRun"`
	Summary   string   `json:"summary"`
	Warning   string   `json:"warning,omitempty"`
	Affected  []string `json:"affectedInstances"`
	Status    Status   `json:"status"`
}
type Options struct {
	Dir, Binary, Module string
	Registry            Registry
}
type supervisor struct {
	mu        sync.Mutex
	options   Options
	snapshot  Snapshot
	child     *child
	lock      *os.File
	backend   int
	cancel    context.CancelFunc
	lastError string
}

func Run(ctx context.Context, o Options) error {
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return err
	}
	o.Dir = dir
	if _, err := load(dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("instance already owned by a supervisor or its draining Envoy: %w", err)
	}
	s, err := load(dir)
	if err != nil {
		return err
	}
	if err := checkBinary(o.Binary); err != nil {
		return err
	}
	if _, err := os.Stat(o.Module); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// One owned echo server lives across Envoy replacements.
	backend, err := reserve()
	if err != nil {
		return err
	}
	echo := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "marker": r.Header.Get("x-fig-marker"), "policy": r.Header.Get("x-fig-policy")})
	})}
	go func() { _ = echo.Serve(backend) }()
	defer echo.Close()
	sup := &supervisor{options: o, snapshot: s, lock: lock, backend: backend.Addr().(*net.TCPAddr).Port, cancel: cancel}
	defer func() { sup.mu.Lock(); defer sup.mu.Unlock(); sup.child.stop() }()
	probe, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if err != nil {
		return fmt.Errorf("proxy port unavailable: %w", err)
	}
	probe.Close()
	data, err := runtimeConfig(s, o.Registry, o.Module, 1, sup.backend)
	if err != nil {
		return err
	}
	if err := validate(o.Binary, data); err != nil {
		return err
	}
	if err := sup.launch(s); err != nil {
		return err
	}
	// The committed pointer is authoritative after interrupted activation. Guardian
	// ownership has ended before the flock can be acquired, so no PID guessing occurs.
	if err := os.Remove(filepath.Join(dir, "pending.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	socket := filepath.Join(dir, "control.sock")
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("control path is not a socket")
		}
		if err := os.Remove(socket); err != nil {
			return err
		}
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 90 * time.Second, Handler: http.HandlerFunc(sup.handle)}
	serveErr := make(chan error, 1)
	_ = json.NewEncoder(os.Stdout).Encode(sup.status())
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != http.ErrServerClosed {
			return err
		}
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return err
	}
	sup.mu.Lock()
	sup.child.stop()
	sup.mu.Unlock()
	return nil
}
func (s *supervisor) status() Status {
	st := Status{Revision: s.snapshot.Revision, Desired: s.snapshot.Revision, Ready: s.child.alive(), URL: fmt.Sprintf("http://127.0.0.1:%d", s.snapshot.Port), Activation: "restart-envoy", LastError: s.lastError}
	if s.child != nil {
		st.PID = s.child.pid
		if !st.Ready && st.LastError == "" {
			st.LastError = "Envoy exited; restart the supervisor"
		}
	}
	return st
}
func (s *supervisor) launch(snapshot Snapshot) error {
	admin, err := reserve()
	if err != nil {
		return err
	}
	defer admin.Close()
	port := admin.Addr().(*net.TCPAddr).Port
	data, err := runtimeConfig(snapshot, s.options.Registry, s.options.Module, port, s.backend)
	if err != nil {
		return err
	}
	path := filepath.Join(s.options.Dir, "envoy.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	// Only the last launch log is retained; it is bounded by local runtime duration.
	log := filepath.Join(s.options.Dir, "envoy.log")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		return err
	}
	admin.Close()
	p, err := startChild(s.options.Binary, path, log, s.lock)
	if err != nil {
		return err
	}
	s.child = p
	if err := ready(p, port); err != nil {
		p.stop()
		return err
	}
	return nil
}
func (s *supervisor) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
		w.WriteHeader(404)
		return
	}
	raw, err := readBounded(r.Body, 65536)
	if err != nil {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(Response{Error: err.Error()})
		return
	}
	var request Request
	if err := bundle.DecodeSpec(raw, &request); err != nil {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(Response{Error: err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.dispatch(request)
	response := Response{Result: result}
	if err != nil {
		response.Error = err.Error()
		w.WriteHeader(400)
	}
	_ = json.NewEncoder(w).Encode(response)
}
func (s *supervisor) dispatch(r Request) (any, error) {
	if r.Op != "action" && (r.Action != "" || r.Instance != "" || r.Input != nil || r.DryRun || r.Help || r.IfRevision != "") {
		return nil, fmt.Errorf("action fields require action operation")
	}
	switch r.Op {
	case "status":
		return s.status(), nil
	case "inspect":
		bindings, err := s.snapshot.Bindings(s.options.Registry)
		return map[string]any{"status": s.status(), "snapshot": s.snapshot, "bindings": bindings}, err
	case "down":
		s.cancel()
		return map[string]string{"state": "stopping"}, nil
	case "action":
	default:
		return nil, fmt.Errorf("unknown operation")
	}
	if r.Action == "" {
		if r.Input != nil || r.DryRun || r.Help || r.IfRevision != "" {
			return nil, fmt.Errorf("action ID required")
		}
		result := map[string][]action.Descriptor{}
		for _, i := range s.snapshot.Instances {
			if r.Instance != "" && i.ID != r.Instance {
				continue
			}
			for _, h := range s.options.Registry[i.Factory].Actions {
				result[i.ID] = append(result[i.ID], h.Descriptor())
			}
		}
		if r.Instance != "" && len(result) == 0 {
			return nil, fmt.Errorf("unknown instance")
		}
		return result, nil
	}
	var selected *Instance
	index := 0
	for n := range s.snapshot.Instances {
		if s.snapshot.Instances[n].ID == r.Instance {
			selected = &s.snapshot.Instances[n]
			index = n
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("an existing --instance is required")
	}
	var handler action.Handler
	for _, h := range s.options.Registry[selected.Factory].Actions {
		if h.Descriptor().ID == r.Action {
			handler = h
			break
		}
	}
	if handler == nil {
		return nil, fmt.Errorf("action unavailable on this instance")
	}
	if r.Help {
		return handler.Descriptor(), nil
	}
	if r.IfRevision != "" && r.IfRevision != s.snapshot.Revision {
		return nil, fmt.Errorf("stale revision")
	}
	if !s.child.alive() {
		return nil, fmt.Errorf("Envoy unavailable; restart supervisor before applying actions")
	}
	next := s.snapshot.Clone()
	change, err := handler.Apply(next.Instances[index].Config, r.Input)
	if err != nil {
		return nil, err
	}
	next.Instances[index].Config = change.Config
	if change.Changed {
		next.Seal()
	}
	result := ActionResult{Operation: fmt.Sprintf("op-%d", time.Now().UnixNano()), Base: s.snapshot.Revision, Revision: next.Revision, Changed: change.Changed, DryRun: r.DryRun, Summary: change.Summary, Status: s.status(), Affected: []string{}}
	if !change.Changed {
		return result, nil
	}
	for _, i := range next.Instances {
		result.Affected = append(result.Affected, i.ID)
	}
	result.Warning = "Restarts Envoy; brief interruption and in-flight request termination are possible."
	data, err := runtimeConfig(next, s.options.Registry, s.options.Module, 1, s.backend)
	if err != nil {
		return nil, err
	}
	if err := validate(s.options.Binary, data); err != nil {
		return nil, err
	}
	if r.DryRun {
		return result, nil
	}
	if err := saveSnapshot(s.options.Dir, next); err != nil {
		return nil, err
	}
	pending := map[string]string{"operation": result.Operation, "base": s.snapshot.Revision, "candidate": next.Revision}
	if err := atomicJSON(filepath.Join(s.options.Dir, "pending.json"), pending); err != nil {
		return nil, err
	}
	s.child.stop()
	activationErr := s.launch(next)
	if activationErr == nil {
		activationErr = atomicJSON(filepath.Join(s.options.Dir, "active.json"), next.Revision)
	}
	if activationErr != nil {
		s.child.stop()
		// If pointer replacement succeeded but its directory sync failed, restore the
		// snapshot named by that pointer. Never run bytes different from committed state.
		committed, loadErr := load(s.options.Dir)
		if loadErr == nil {
			s.snapshot = committed
			loadErr = s.launch(committed)
		}
		s.lastError = fmt.Sprintf("activation failed: %v; restoration error: %v", activationErr, loadErr)
		result.Status = s.status()
		return result, fmt.Errorf("%s", s.lastError)
	}
	s.snapshot = next
	s.lastError = ""
	if err := os.Remove(filepath.Join(s.options.Dir, "pending.json")); err != nil {
		s.lastError = "applied; pending journal cleanup failed: " + err.Error()
	}
	result.Status = s.status()
	return result, nil
}
func readBounded(r io.Reader, max int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(max+1)))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, fmt.Errorf("input exceeds %d bytes", max)
	}
	return data, nil
}
func Call(ctx context.Context, dir string, request Request) (Response, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "control.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 75 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://local/rpc", bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	raw, err := readBounded(res.Body, 8*bundle.MaxBytes)
	if err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}
