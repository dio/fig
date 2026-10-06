package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dio/fig/hosts/envoy/bootstrap"
)

// Child runs in a guardian process. The parent pipe closes even on supervisor SIGKILL.
// FD 3 retains the supervisor's flock until Envoy has been reaped.
func Child(args []string) error {
	// A supervisor may die before reading the PID handshake. Keep the guardian
	// alive on a broken stdout pipe so it can still reap Envoy on stdin EOF.
	signal.Ignore(syscall.SIGPIPE)
	if len(args) != 3 {
		return fmt.Errorf("invalid internal child arguments")
	}
	lock := os.NewFile(3, "lock")
	defer lock.Close()
	log, err := os.OpenFile(args[2], os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(args[0], "-c", args[1], "--concurrency", "2", "--log-level", "warning", "--disable-hot-restart")
	cmd.Env = append(os.Environ(), "GODEBUG=cgocheck=0")
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	_ = json.NewEncoder(os.Stdout).Encode(cmd.Process.Pid)
	closed := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(closed) }()
	select {
	case err := <-done:
		return err
	case <-closed:
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return nil
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}

type child struct {
	pipe io.WriteCloser
	done chan struct{}
	pid  int
}

func (p *child) stop() {
	if p == nil {
		return
	}
	_ = p.pipe.Close()
	<-p.done
}
func (p *child) alive() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func startChild(binary, path, log string, lock *os.File) (*child, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "internal-child", binary, path, log)
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		return nil, err
	}
	p := &child{pipe: in, done: make(chan struct{})}
	// Guardian starts Envoy immediately and emits only its PID on stdout.
	pidReady := make(chan error, 1)
	go func() { pidReady <- json.NewDecoder(out).Decode(&p.pid) }()
	select {
	case err = <-pidReady:
	case <-time.After(5 * time.Second):
		err = fmt.Errorf("guardian startup timed out")
		_ = in.Close()
	}
	go func() { _ = cmd.Wait(); close(p.done) }()
	if err != nil {
		p.stop()
		return nil, err
	}
	return p, nil
}
func runtimeConfig(s Snapshot, reg Registry, module string, admin, backend int) ([]byte, error) {
	bindings, err := s.Bindings(reg)
	if err != nil {
		return nil, err
	}
	raw, err := bootstrap.Render([]byte(bootstrap.Template), bindings...)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				switch x := x.(type) {
				case string:
					switch x {
					case "__FIG_MODULE__":
						v[k] = module
					case "__FIG_BACKEND__", "0.0.0.0":
						v[k] = "127.0.0.1"
					}
				case float64:
					if k == "port_value" {
						switch int(x) {
						case 9901:
							v[k] = admin
						case 8080:
							v[k] = backend
						case 10000:
							v[k] = s.Port
						}
					}
				default:
					walk(x)
				}
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(tree)
	return json.MarshalIndent(tree, "", "  ")
}
func validate(binary string, data []byte) error {
	dir, err := os.MkdirTemp("", "fig-validate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "envoy.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--mode", "validate", "-c", path, "--concurrency", "2", "--disable-hot-restart")
	cmd.Env = append(os.Environ(), "GODEBUG=cgocheck=0")
	// Bound diagnostics even when a broken runtime is noisy.
	output := &boundedLog{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Envoy validation: %w: %s", err, output.data)
	}
	return nil
}

type boundedLog struct{ data []byte }

func (b *boundedLog) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 32768 - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(len(p), remaining)]...)
	}
	return n, nil
}
func reserve() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }
func ready(p *child, port int) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	defer client.CloseIdleConnections()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return fmt.Errorf("Envoy exited; inspect envoy.log")
		case <-deadline.C:
			return fmt.Errorf("Envoy readiness timed out")
		case <-tick.C:
			r, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/ready", port))
			if err == nil {
				r.Body.Close()
				if r.StatusCode == 200 && p.alive() {
					return nil
				}
			}
		}
	}
}
func checkBinary(binary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), "0a804c57") {
		return fmt.Errorf("matching Envoy commit 0a804c57 required")
	}
	return nil
}
