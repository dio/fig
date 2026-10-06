package envoy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dio/fig/hosts/envoy/internal/local"
)

func TestNativeServe(t *testing.T) {
	cli, envoy := os.Getenv("FIG_CLI"), os.Getenv("ENVOY_BIN")
	if cli == "" || envoy == "" {
		t.Skip("set FIG_CLI and ENVOY_BIN")
	}
	root, err := os.MkdirTemp("/tmp", "fig-serve-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "state")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	command := func(stdin string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Stdin = strings.NewReader(stdin)
		return cmd.CombinedOutput()
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, err := command("", args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	run("init", "--dir", dir, "--port", fmt.Sprint(port))
	if _, err := command("", "init", "--dir", dir); err == nil {
		t.Fatal("overwrote existing instance")
	}
	var cmd *exec.Cmd
	var done chan error
	stop := func() {
		if cmd == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		cmd = nil
	}
	defer stop()
	getStatus := func() (local.Status, error) {
		res, err := local.Call(context.Background(), dir, local.Request{Op: "status"})
		if err != nil {
			return local.Status{}, err
		}
		raw, _ := json.Marshal(res.Result)
		var st local.Status
		err = json.Unmarshal(raw, &st)
		return st, err
	}
	start := func(binary string) {
		t.Helper()
		log, err := os.OpenFile(filepath.Join(root, "supervisor.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command(cli, "serve", "--dir", dir, "up", "--envoy", binary, "--module", os.Getenv("FIG_MODULE"))
		cmd.Stdout = log
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done = make(chan error, 1)
		go func(c *exec.Cmd, ch chan error) { ch <- c.Wait(); log.Close() }(cmd, done)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			st, err := getStatus()
			if err == nil && st.Ready {
				return
			}
			time.Sleep(40 * time.Millisecond)
		}
		data, _ := os.ReadFile(filepath.Join(root, "supervisor.log"))
		t.Fatalf("not ready: %s", data)
	}
	status := func() local.Status {
		t.Helper()
		st, err := getStatus()
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	request := func(attack bool, want int, marker string) {
		t.Helper()
		client := &http.Client{Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/headers", port), nil)
		if attack {
			req.Header.Set("X-Fig-Attack", "attack")
		}
		req.Header.Set("x-fig-marker", "spoof")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want || res.Header.Get("x-fig-marker") != marker {
			t.Fatalf("status %d marker %q; wanted %d %q", res.StatusCode, res.Header.Get("x-fig-marker"), want, marker)
		}
	}
	actionArgs := func(input string, extra ...string) []string {
		return append([]string{"serve", "--dir", dir, "action", "waf:SetMode", "--instance", "waf-a", "--input", input}, extra...)
	}
	detect := `{"policy":"baseline","mode":"detect"}`
	enforce := `{"policy":"baseline","mode":"enforce"}`
	start(envoy)
	if out, err := command("", "serve", "--dir", dir, "up", "--envoy", envoy, "--module", os.Getenv("FIG_MODULE")); err == nil || !strings.Contains(string(out), "already owned") {
		t.Fatal("second owner accepted", string(out), err)
	}
	initial := status()
	request(true, 403, "")
	request(false, 200, "waf-clean")
	run("serve", "--dir", dir, "action")
	help := run("serve", "--dir", dir, "action", "waf:SetMode", "--instance", "waf-a", "--help")
	if !strings.Contains(string(help), "inputSchema") {
		t.Fatal("missing discovery schema")
	}
	activeBefore, _ := os.ReadFile(filepath.Join(dir, "active.json"))
	configBefore, _ := os.ReadFile(filepath.Join(dir, "envoy.json"))
	run(actionArgs(detect, "--dry-run")...)
	if got := status(); got.PID != initial.PID || got.Revision != initial.Revision {
		t.Fatal("preview mutated state")
	}
	activeAfter, _ := os.ReadFile(filepath.Join(dir, "active.json"))
	configAfter, _ := os.ReadFile(filepath.Join(dir, "envoy.json"))
	if string(activeBefore) != string(activeAfter) || string(configBefore) != string(configAfter) {
		t.Fatal("preview wrote config")
	}
	request(true, 403, "")
	if _, err := command("", actionArgs(`{"policy":"baseline","mode":"off"}`)...); err == nil {
		t.Fatal("invalid mode accepted")
	}
	run(actionArgs(detect, "--if-revision", initial.Revision)...)
	detected := status()
	if detected.PID == initial.PID || detected.Revision == initial.Revision {
		t.Fatal("did not replace")
	}
	request(true, 200, "waf-detected")
	run(actionArgs(detect)...)
	if status().PID != detected.PID {
		t.Fatal("no-op restarted")
	}
	if _, err := command("", actionArgs(enforce, "--if-revision", initial.Revision)...); err == nil {
		t.Fatal("stale revision accepted")
	}
	markerInput := `{"match":"select-marker","rule":"detected","value":"review-needed"}`
	file := filepath.Join(root, "marker.json")
	if err := os.WriteFile(file, []byte(markerInput), 0600); err != nil {
		t.Fatal(err)
	}
	run("serve", "--dir", dir, "action", "marker:SetValue", "--instance", "marker-a", "--input", "@"+file)
	request(true, 200, "review-needed")
	// Exact-revision concurrency: one writer wins; the other must reread.
	revision := status().Revision
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := local.Call(context.Background(), dir, local.Request{Op: "action", Action: "waf:SetMode", Instance: "waf-a", Input: json.RawMessage(enforce), IfRevision: revision})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("concurrent successes", successes)
	}
	request(true, 403, "")
	saved := status().Revision
	run("serve", "--dir", dir, "down")
	stop()
	start(envoy)
	if status().Revision != saved {
		t.Fatal("restart lost actions")
	}
	// Stdin input follows the same action path.
	out, err := command(detect, "serve", "--dir", dir, "action", "waf:SetMode", "--instance", "waf-a", "--input", "@-")
	if err != nil {
		t.Fatal(err, string(out))
	}
	request(true, 200, "review-needed")
	// Supervisor crash closes the guardian's pipe; owned Envoy is reaped before lock release.
	old := status()
	_ = cmd.Process.Kill()
	<-done
	cmd = nil
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(old.PID, 0); err != nil {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if err := syscall.Kill(old.PID, 0); err == nil {
		t.Fatal("Envoy survived supervisor crash")
	}
	start(envoy)
	request(true, 200, "review-needed")
	run("serve", "--dir", dir, "down")
	stop()
	// Fail one real replacement after successful native validation; old snapshot restores.
	fail := filepath.Join(root, "fail-next")
	failAll := filepath.Join(root, "fail-all")
	wrapper := filepath.Join(root, "envoy-wrapper")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nif [ \"$1\" = '-c' ] && [ -f " + quote(fail) + " ]; then rm " + quote(fail) + "; exit 23; fi\nexec " + quote(envoy) + " \"$@\"\n"
	script = strings.Replace(script, "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1\" = '-c' ] && [ -f "+quote(failAll)+" ]; then exit 24; fi\n", 1)
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	start(wrapper)
	beforeFailure := status()
	if err := os.WriteFile(fail, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := command("", actionArgs(enforce)...); err == nil {
		t.Fatal("failed launch reported success")
	}
	afterFailure := status()
	if !afterFailure.Ready || afterFailure.Revision != beforeFailure.Revision || afterFailure.LastError == "" {
		t.Fatal("restoration not reported", afterFailure)
	}
	request(true, 200, "review-needed")
	if err := os.WriteFile(failAll, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := command("", actionArgs(enforce)...); err == nil {
		t.Fatal("failed restoration reported success")
	}
	if st := status(); st.Ready || st.LastError == "" {
		t.Fatal("unavailability hidden", st)
	}
	if _, err := command("", actionArgs(detect)...); err == nil {
		t.Fatal("mutation allowed while unavailable")
	}
	run("serve", "--dir", dir, "down")
	stop()
	// Recovery clears pending activation and resumes the committed pointer.
	start(envoy)
	if status().Revision != beforeFailure.Revision {
		t.Fatal("resumed failed candidate")
	}
	request(true, 200, "review-needed")
}

func TestGuardianBrokenHandshake(t *testing.T) {
	cli := os.Getenv("FIG_CLI")
	if cli == "" {
		t.Skip("set FIG_CLI")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	shim := filepath.Join(dir, "child")
	script := "#!/bin/sh\necho $$ > '" + strings.ReplaceAll(pidFile, "'", "'\\''") + "'\nexec /bin/sleep 30\n"
	if err := os.WriteFile(shim, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.Create(filepath.Join(dir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cmd := exec.Command(cli, "internal-child", shim, "unused", filepath.Join(dir, "log"))
	cmd.ExtraFiles = []*os.File{lock}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	output.Close() // no PID reader
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var pid int
	defer func() {
		if pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			_, _ = fmt.Sscanf(string(raw), "%d", &pid)
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		input.Close()
		<-done
		t.Fatal("child did not start")
	}
	input.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("guardian died on broken handshake", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("guardian did not stop")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("child not reaped")
	}
	pid = 0
}
