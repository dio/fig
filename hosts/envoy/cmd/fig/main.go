// Command fig runs the local native demo and dispatches app-owned actions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dio/fig/action"
	"github.com/dio/fig/apps/marker"
	markercontrol "github.com/dio/fig/apps/marker/control"
	"github.com/dio/fig/apps/waf"
	wafcontrol "github.com/dio/fig/apps/waf/control"
	"github.com/dio/fig/bundle"
	examples "github.com/dio/fig/examples/config"
	"github.com/dio/fig/handoff"
	"github.com/dio/fig/hosts/envoy/internal/local"
	"github.com/dio/fig/match"
)

func registry() local.Registry {
	return local.Registry{
		"fig-waf-app": {Actions: []action.Handler{wafcontrol.SetMode{}}, Prepare: func(c action.Config) error { _, err := waf.Compile(c.Bundle, c.Entry); return err }},
		"fig-marker-app": {Actions: []action.Handler{markercontrol.SetValue{}}, Prepare: func(c action.Config) error {
			_, err := marker.CompileWithInputs(c.Bundle, c.Entry, c.Inputs)
			return err
		}},
	}
}
func preset(port int) local.Snapshot {
	slot := handoff.Slot{Activation: "initial", Placement: "edge", Producer: "waf-a", Name: "inspection", Type: waf.OutcomeType, Scope: "demo", Generation: "1", Representation: "request-headers@waf-entry", Phase: match.Headers}
	return local.Snapshot{Port: port, Instances: []local.Instance{
		{ID: "waf-a", Name: "WAF", Factory: "fig-waf-app", Config: action.Config{Entry: bundle.Ref{Type: waf.PipelineType, Name: "edge", Version: "1"}, Bundle: examples.WAF}, Export: &slot},
		{ID: "marker-a", Name: "MARKER", Factory: "fig-marker-app", Config: action.Config{Entry: bundle.Ref{Type: marker.PipelineType, Name: "mark", Version: "1"}, Bundle: examples.MarkerInspection, Inputs: map[string]match.Kind{"input.inspection": match.Boolean}}, Input: &handoff.Input{Slot: slot, Field: "matched", Fact: "input.inspection", Kind: match.Boolean, Required: true}},
	}}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fig:", err)
		os.Exit(1)
	}
}
func flags(name string) *flag.FlagSet { return flag.NewFlagSet(name, flag.ContinueOnError) }
func run(args []string) error {
	if len(args) > 0 && args[0] == "internal-child" {
		return local.Child(args[1:])
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: fig demo init --dir DIR | fig serve --dir DIR up|status|inspect|action|down")
	}
	if len(args) >= 2 && args[0] == "demo" && args[1] == "init" {
		f := flags("demo init")
		dir := f.String("dir", "", "new private demo directory")
		port := f.Int("port", 18080, "loopback proxy port")
		if err := f.Parse(args[2:]); err != nil {
			return err
		}
		if *dir == "" || f.NArg() != 0 {
			return fmt.Errorf("--dir required")
		}
		s := preset(*port)
		s.Seal()
		if _, err := s.Bindings(registry()); err != nil {
			return err
		}
		if err := local.Init(*dir, s); err != nil {
			return err
		}
		fmt.Println("Initialized", *dir)
		return nil
	}
	if args[0] != "serve" {
		return fmt.Errorf("expected demo init or serve")
	}
	f := flags("serve")
	dir := f.String("dir", "", "demo directory")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	rest := f.Args()
	if *dir == "" || len(rest) == 0 {
		return fmt.Errorf("serve --dir DIR and command required")
	}
	op := rest[0]
	rest = rest[1:]
	if op == "up" {
		sub := flags("up")
		envoy := sub.String("envoy", "", "matching Envoy binary")
		module := sub.String("module", "", "matching dynamic module")
		if err := sub.Parse(rest); err != nil {
			return err
		}
		if sub.NArg() != 0 {
			return fmt.Errorf("unexpected arguments")
		}
		if *envoy == "" {
			*envoy = os.Getenv("ENVOY_BIN")
		}
		if *envoy == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			*envoy = filepath.Join(home, ".tetrate", "bin", "envoy")
		}
		binary, err := exec.LookPath(*envoy)
		if err != nil {
			return err
		}
		binary, err = filepath.Abs(binary)
		if err != nil {
			return err
		}
		if *module == "" {
			self, err := os.Executable()
			if err != nil {
				return err
			}
			*module = filepath.Join(filepath.Dir(self), "libfig_match.so")
		}
		modulePath, err := filepath.Abs(*module)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return local.Run(ctx, local.Options{Dir: *dir, Binary: binary, Module: modulePath, Registry: registry()})
	}
	req := local.Request{Op: op}
	if op == "action" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		req.Action = rest[0]
		rest = rest[1:]
	}
	sub := flags(op)
	sub.StringVar(&req.Instance, "instance", "", "configured app instance")
	sub.BoolVar(&req.DryRun, "dry-run", false, "validate without applying")
	sub.StringVar(&req.IfRevision, "if-revision", "", "require exact composition revision")
	sub.BoolVar(&req.Help, "help", false, "show app-owned input schema")
	input := sub.String("input", "", "inline JSON, @file, or @- for stdin")
	_ = sub.Bool("json", false, "structured output (the default)")
	if err := sub.Parse(rest); err != nil {
		return err
	}
	if sub.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	switch op {
	case "action", "status", "inspect", "down":
	default:
		return fmt.Errorf("unknown command %q", op)
	}
	if op != "action" && (req.DryRun || req.Help || req.Instance != "" || *input != "" || req.IfRevision != "") {
		return fmt.Errorf("action flags require action")
	}
	if *input != "" {
		var reader io.Reader = strings.NewReader(*input)
		if strings.HasPrefix(*input, "@") {
			if *input == "@-" {
				reader = os.Stdin
			} else {
				file, err := os.Open((*input)[1:])
				if err != nil {
					return err
				}
				defer file.Close()
				reader = file
			}
		}
		raw, err := io.ReadAll(io.LimitReader(reader, 32769))
		if err != nil {
			return err
		}
		if len(raw) > 32768 {
			return fmt.Errorf("action input exceeds 32768 bytes")
		}
		req.Input = raw
	}
	response, err := local.Call(context.Background(), *dir, req)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if response.Result != nil || response.Error != "" {
		_ = enc.Encode(response)
	}
	if err != nil {
		return err
	}
	if op == "down" {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(*dir, "control.sock")); os.IsNotExist(err) {
				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
		return fmt.Errorf("shutdown still pending")
	}
	return nil
}
