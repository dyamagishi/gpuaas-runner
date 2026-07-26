package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/olduvai-jp/gpu-run/internal/provider"
	"github.com/olduvai-jp/gpu-run/internal/recipe"
	"github.com/olduvai-jp/gpu-run/internal/runner"
	"github.com/olduvai-jp/gpu-run/internal/state"
	"gopkg.in/yaml.v3"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	exitOK        = 0
	exitInput     = 2
	exitProvision = 3
	exitRemote    = 4
	exitArtifact  = 5
	exitCleanup   = 6
)

func main() { os.Exit(run(os.Args[1:])) }
func run(a []string) int {
	loadDotEnv(".env")
	if len(a) == 0 {
		usage()
		return exitInput
	}
	switch a[0] {
	case "config":
		return config(a[1:])
	case "recipe":
		return recipeCmd(a[1:])
	case "run":
		return runCmd(a[1:])
	case "runs":
		return runs(a[1:])
	case "status", "attach", "cancel", "recover", "cleanup":
		return lifecycle(a[0], a[1:])
	default:
		usage()
		return exitInput
	}
}

// loadDotEnv loads simple KEY=VALUE entries without overwriting the process environment.
// Secrets are never printed or persisted by the CLI.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
func usage() {
	fmt.Fprintln(os.Stderr, "gpu-run config init|recipe validate|run|runs list|status|attach|cancel|recover|cleanup")
}
func config(a []string) int {
	if len(a) != 1 || a[0] != "init" {
		return exitInput
	}
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return exitInput
	}
	if _, e := os.Stat(p); e == nil {
		fmt.Println(p)
		return exitOK
	}
	v := []byte("max_hourly_usd: 1\nmax_runtime: 3600s\nmax_disk_gb: 100\nallowed_gpu_ids: []\n")
	if e := os.WriteFile(p, v, 0600); e != nil {
		return exitInput
	}
	fmt.Println(p)
	return exitOK
}
func recipeCmd(a []string) int {
	if len(a) < 2 || a[0] != "validate" {
		return exitInput
	}
	file := a[1]
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	inputs := multiFlag{}
	fs.Var(&inputs, "input", "name=value")
	if e := fs.Parse(a[2:]); e != nil {
		return exitInput
	}
	r, e := recipe.Load(file)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return exitInput
	}
	vals := parseInputs(inputs)
	if _, e = recipe.ResolveInputs(r, vals); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return exitInput
	}
	fmt.Printf("valid recipe %s (%s)\n", r.Name, recipe.Digest(r))
	return exitOK
}
func runCmd(a []string) int {
	if len(a) < 1 {
		return exitInput
	}
	file := a[0]
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	inputs := multiFlag{}
	fs.Var(&inputs, "input", "name=value")
	out := fs.String("output", "", "output dir")
	detach := fs.Bool("detach", false, "detach after submission")
	if e := fs.Parse(a[1:]); e != nil {
		return exitInput
	}
	if *detach {
		fmt.Fprintln(os.Stderr, "--detach is not available until attach/recover state persistence is implemented")
		return exitProvision
	}
	r, e := recipe.Load(file)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return exitInput
	}
	vals, e := recipe.ResolveInputs(r, parseInputs(inputs))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return exitInput
	}
	if v, ok := vals["output_name"]; ok {
		name := fmt.Sprint(v)
		if filepath.Base(name) != name || name == "." || name == ".." || name == "" {
			fmt.Fprintln(os.Stderr, "output_name must be a single safe path component")
			return exitInput
		}
	}
	id := newID()
	if *out == "" {
		*out = filepath.Join("runs", r.Name, id)
	}
	if e = os.MkdirAll(*out, 0700); e != nil {
		return exitInput
	}
	s, e := state.Open(state.Path())
	if e != nil {
		return exitInput
	}
	defer s.DB.Close()
	now := time.Now()
	st := state.Run{ID: id, Recipe: r.Name, Phase: "provisioning", OutputDir: *out, CreatedAt: now, UpdatedAt: now}
	if _, e = os.Stat(configPath()); e != nil || os.Getenv("RUNPOD_API_KEY") == "" {
		st.Phase = "provisioning_unavailable"
		st.Error = "provider configuration or RUNPOD_API_KEY missing"
		e = s.Put(st)
		if e != nil {
			return exitInput
		}
		fmt.Fprintf(os.Stderr, "run %s blocked: %s\n", id, st.Error)
		return exitProvision
	}
	p, e := provider.NewRunpodctl()
	if e != nil {
		st.Phase = "provisioning_failed"
		st.Error = e.Error()
		_ = s.Put(st)
		return exitProvision
	}
	cfg := struct {
		MaxRuntime    string   `yaml:"max_runtime"`
		MaxDiskGB     int      `yaml:"max_disk_gb"`
		MaxHourlyUSD  float64  `yaml:"max_hourly_usd"`
		AllowedGPUIDs []string `yaml:"allowed_gpu_ids"`
	}{}
	if b, er := os.ReadFile(configPath()); er == nil {
		_ = yaml.Unmarshal(b, &cfg)
	}
	d, durationErr := time.ParseDuration(cfg.MaxRuntime)
	if durationErr != nil || d <= 0 || cfg.MaxDiskGB <= 0 || cfg.MaxHourlyUSD <= 0 || len(cfg.AllowedGPUIDs) == 0 {
		st.Phase = "provisioning_failed"
		st.Error = "config hard limits must include positive max_runtime, max_disk_gb, max_hourly_usd, and allowed_gpu_ids"
		_ = s.Put(st)
		return exitProvision
	}
	runtimeSec := int64(d / time.Second)
	if r.Requirements.DiskGB > cfg.MaxDiskGB {
		st.Phase = "provisioning_failed"
		st.Error = "recipe disk exceeds configured hard limit"
		_ = s.Put(st)
		return exitProvision
	}
	for _, need := range append(append([]string{}, r.Requirements.Env...), flattenEnv(r.Stages)...) {
		if os.Getenv(need) == "" {
			st.Phase = "provisioning_failed"
			st.Error = "required environment variable missing: " + need
			_ = s.Put(st)
			return exitProvision
		}
	}
	allowed := map[string]bool{}
	for _, g := range cfg.AllowedGPUIDs {
		allowed[g] = true
	}
	gpuIDs := append([]string(nil), r.Requirements.AllowedGPU...)
	if len(allowed) > 0 {
		if len(gpuIDs) == 0 {
			gpuIDs = append(gpuIDs, cfg.AllowedGPUIDs...)
		}
		if len(r.Requirements.AllowedGPU) > 0 {
			ok := false
			gpuIDs = gpuIDs[:0]
			for _, g := range r.Requirements.AllowedGPU {
				if allowed[g] {
					ok = true
					gpuIDs = append(gpuIDs, g)
				}
			}
			if !ok {
				st.Phase = "provisioning_failed"
				st.Error = "no GPU satisfies allowed set"
				_ = s.Put(st)
				return exitProvision
			}
		}
	}
	if len(r.Stages) != 1 {
		st.Phase = "provisioning_failed"
		st.Error = "v1 supports exactly one stage; multi-stage orchestration is deferred"
		_ = s.Put(st)
		return exitProvision
	}
	for name, in := range r.Inputs {
		if in.Secret {
			st.Phase = "provisioning_failed"
			st.Error = fmt.Sprintf("secret input %q is not supported in v1; no Pod created", name)
			_ = s.Put(st)
			return exitProvision
		}
	}
	if len(r.Requirements.Env) > 0 || len(flattenEnv(r.Stages)) > 0 {
		st.Phase = "provisioning_failed"
		st.Error = "remote secret/environment injection is deferred; no Pod created"
		_ = s.Put(st)
		return exitProvision
	}
	for _, t := range r.Transfers {
		v, ok := vals[t.Input]
		if !ok || strings.TrimSpace(fmt.Sprint(v)) == "" || fmt.Sprint(v) == "<nil>" {
			st.Phase = "provisioning_failed"
			st.Error = fmt.Sprintf("transfer input %q is missing; no Pod created", t.Input)
			_ = s.Put(st)
			return exitInput
		}
	}
	reqGPU := r.Requirements.GPUs
	if reqGPU == 0 {
		reqGPU = r.Requirements.GPUCount
	}
	pod, e := p.CreatePod(context.Background(), provider.PodRequest{Name: "gpu-run-" + id, Image: r.Image, GPUs: reqGPU, GPUIDs: gpuIDs, DiskGB: r.Requirements.DiskGB, TerminateAfter: runtimeSec, PublicSSH: true})
	if e != nil {
		st.Phase = "provisioning_failed"
		st.Error = e.Error()
		_ = s.Put(st)
		return exitProvision
	}
	if cfg.MaxHourlyUSD > 0 && (pod.HourlyUSD <= 0 || pod.HourlyUSD > cfg.MaxHourlyUSD) {
		st.Phase = "provisioning_failed"
		st.Error = "pod hourly price exceeds hard limit or unavailable"
		if de := p.DeletePod(context.Background(), pod.ID); de != nil {
			st.Phase = "cleanup_required"
			st.Error = de.Error()
			_ = s.Put(st)
			return exitCleanup
		}
		_ = s.Put(st)
		return exitProvision
	}
	st.ProviderID = pod.ID
	podActive := true
	defer func() {
		if podActive {
			if de := p.DeletePod(context.Background(), pod.ID); de != nil {
				st.Phase = "cleanup_required"
				st.Error = de.Error()
				_ = s.Put(st)
			}
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st.Phase = "running"
	st.UpdatedAt = time.Now()
	_ = s.Put(st)
	si, e := p.SSHInfo(ctx, pod.ID)
	if e != nil {
		st.Phase = "remote_failed"
		st.Error = e.Error()
		_ = s.Put(st)
		if de := p.DeletePod(ctx, pod.ID); de != nil {
			st.Phase = "cleanup_required"
			st.Error = de.Error()
			_ = s.Put(st)
			return exitCleanup
		}
		podActive = false
		return exitRemote
	}
	transfers := map[string]string{}
	job := runner.Job{RunID: id, RemoteDir: "/workspace/gpu-run/runs/" + id, ArtifactRoot: "/workspace/gpu-run/runs/" + id, SSH: runner.SSH{Host: si.Host, Port: si.Port, User: si.User, Key: si.PrivateKey, KnownHosts: si.KnownHosts}}
	for _, t := range r.Transfers {
		v := fmt.Sprint(vals[t.Input])
		remote := t.RemotePath
		remote, e = recipe.ExpandWithTransfers(remote, mapString(vals), transfers, id)
		if e != nil {
			st.Phase = "transfer_failed"
			st.Error = e.Error()
			_ = s.Put(st)
			return exitInput
		}
		transfers[t.Input] = remote
		source := v
		if r.Inputs[t.Input].Type == "directory" && !strings.HasSuffix(source, string(os.PathSeparator)) {
			source += string(os.PathSeparator)
		}
		if strings.HasPrefix(source, "https://") {
			e = job.FetchURL(ctx, source, remote)
		} else {
			e = job.Transfer(ctx, source, remote)
		}
		if e != nil {
			st.Phase = "transfer_failed"
			st.Error = e.Error()
			_ = s.Put(st)
			return exitRemote
		}
		transfers[t.Input] = filepath.Join(job.RemoteDir, remote)
	}
	for _, stage := range r.Stages {
		av := make([]string, len(stage.Argv))
		for i, x := range stage.Argv {
			av[i], e = recipe.ExpandWithTransfers(x, mapString(vals), transfers, id)
			if e != nil {
				st.Phase = "remote_failed"
				st.Error = e.Error()
				_ = s.Put(st)
				return exitInput
			}
		}
		job.Stages = append(job.Stages, av)
		job.WorkingDirs = append(job.WorkingDirs, stage.WorkingDir)
	}
	for _, a := range r.Artifacts {
		rp, expandErr := recipe.ExpandWithTransfers(a.RemotePath, mapString(vals), transfers, id)
		if expandErr != nil {
			st.Phase = "provisioning_failed"
			st.Error = expandErr.Error()
			_ = s.Put(st)
			return exitInput
		}
		job.Artifacts = append(job.Artifacts, runner.Artifact{Name: a.Name, RemotePath: rp, Kind: a.Kind, Required: a.Required})
	}
	if e = job.Run(ctx); e != nil {
		st.Phase = "remote_failed"
		st.Error = e.Error()
		_ = s.Put(st)
		return exitRemote
	}
	status, pollErr := job.Poll(ctx, time.Duration(runtimeSec)*time.Second)
	if pollErr != nil {
		st.Phase = "remote_failed"
		st.Error = pollErr.Error()
		_ = s.Put(st)
		_ = job.FetchDiagnostics(ctx, *out)
		return exitRemote
	}
	if phase, _ := status["phase"].(string); phase == "failed" || phase == "error" {
		st.Phase = "remote_failed"
		st.Error = fmt.Sprintf("remote runner phase=%s exit_code=%v", phase, status["exit_code"])
		_ = s.Put(st)
		_ = job.FetchDiagnostics(ctx, *out)
		return exitRemote
	}
	if e = job.FetchArtifacts(ctx, *out); e != nil {
		st.Phase = "artifact_failed"
		st.Error = e.Error()
		_ = s.Put(st)
		_ = job.FetchDiagnostics(ctx, *out)
		if de := p.DeletePod(ctx, pod.ID); de != nil {
			st.Phase = "cleanup_required"
			st.Error = de.Error()
			_ = s.Put(st)
			return exitCleanup
		}
		podActive = false
		return exitArtifact
	}
	e = p.DeletePod(context.Background(), pod.ID)
	podActive = false
	st.Phase = "completed"
	if e != nil {
		st.Phase = "cleanup_failed"
		st.Error = e.Error()
	}
	st.UpdatedAt = time.Now()
	_ = s.Put(st)
	if e != nil {
		return exitCleanup
	}
	return exitOK
}
func mapString(v map[string]any) map[string]string {
	o := map[string]string{}
	for k, x := range v {
		o[k] = fmt.Sprint(x)
	}
	return o
}
func flattenEnv(stages []recipe.Stage) []string {
	var out []string
	for _, s := range stages {
		out = append(out, s.EnvFrom...)
	}
	return out
}
func runs(a []string) int {
	if len(a) != 1 || a[0] != "list" {
		return exitInput
	}
	s, e := state.Open(state.Path())
	if e != nil {
		return exitInput
	}
	defer s.DB.Close()
	rs, e := s.List()
	if e != nil {
		return exitInput
	}
	for _, r := range rs {
		fmt.Printf("%s %s %s\n", r.ID, r.Recipe, r.Phase)
	}
	return exitOK
}
func lifecycle(cmd string, a []string) int {
	if len(a) < 1 {
		return exitInput
	}
	id := a[0]
	s, e := state.Open(state.Path())
	if e != nil {
		return exitInput
	}
	defer s.DB.Close()
	r, e := s.Get(id)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return exitInput
	}
	switch cmd {
	case "status":
		fmt.Println(r.JSON())
	case "attach", "recover":
		fmt.Fprintln(os.Stderr, cmd+" is not available until SSH/run metadata persistence is implemented")
		return exitProvision
	case "cancel":
		if r.ProviderID != "" {
			p, pe := provider.NewRunpodctl()
			if pe != nil {
				r.Phase = "cleanup_required"
				r.Error = pe.Error()
				_ = s.Put(r)
				return exitCleanup
			}
			if pe = p.DeletePod(context.Background(), r.ProviderID); pe != nil {
				r.Phase = "cleanup_required"
				r.Error = pe.Error()
				_ = s.Put(r)
				return exitCleanup
			}
		}
		r.Phase = "cancelled"
		r.UpdatedAt = time.Now()
		e = s.Put(r)
	case "cleanup":
		if r.ProviderID != "" {
			p, pe := provider.NewRunpodctl()
			if pe != nil {
				r.Phase = "cleanup_required"
				r.Error = pe.Error()
				_ = s.Put(r)
				return exitCleanup
			}
			if pe = p.DeletePod(context.Background(), r.ProviderID); pe != nil {
				r.Phase = "cleanup_required"
				r.Error = pe.Error()
				_ = s.Put(r)
				return exitCleanup
			}
		}
		r.Phase = "cleaned"
		r.UpdatedAt = time.Now()
		e = s.Put(r)
	}
	if e != nil {
		return exitCleanup
	}
	return exitOK
}
func configPath() string {
	if x := os.Getenv("GPU_RUN_CONFIG"); x != "" {
		return x
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "gpu-run", "config.yaml")
}
func newID() string {
	b := make([]byte, 8)
	if _, e := rand.Read(b); e != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	if !strings.Contains(v, "=") {
		return errors.New("expected name=value")
	}
	*m = append(*m, v)
	return nil
}
func parseInputs(m multiFlag) map[string]string {
	o := map[string]string{}
	for _, x := range m {
		p := strings.SplitN(x, "=", 2)
		if len(p) == 2 {
			o[p[0]] = p[1]
		}
	}
	return o
}

var _ = json.Valid
