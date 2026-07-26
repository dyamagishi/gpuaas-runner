package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runpodctl struct {
	Bin     string
	Env     []string
	Timeout time.Duration
}
type wirePod struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Machine struct {
		PodID string `json:"podId"`
	} `json:"machine"`
	Cost     float64 `json:"costPerHr"`
	SSHHost  string  `json:"sshHost"`
	SSHPort  int     `json:"sshPort"`
	PublicIP string  `json:"publicIp"`
}

func NewRunpodctl() (*Runpodctl, error) {
	b := os.Getenv("GPU_RUNPODCTL")
	if b == "" {
		p, e := exec.LookPath("runpodctl")
		if e != nil {
			return nil, e
		}
		b = p
	}
	return &Runpodctl{Bin: b, Env: []string{"RUNPOD_API_KEY=" + os.Getenv("RUNPOD_API_KEY")}, Timeout: 45 * time.Second}, nil
}
func (r *Runpodctl) call(ctx context.Context, args ...string) ([]byte, error) {
	if _, e := exec.LookPath(r.Bin); e != nil {
		return nil, e
	}
	if r.Timeout == 0 {
		r.Timeout = 45 * time.Second
	}
	ctx, c := context.WithTimeout(ctx, r.Timeout)
	defer c()
	x := exec.CommandContext(ctx, r.Bin, args...)
	x.Env = append(os.Environ(), r.Env...)
	var out, err bytes.Buffer
	x.Stdout = &out
	x.Stderr = &err
	if e := x.Run(); e != nil {
		return nil, fmt.Errorf("runpodctl %v: %w: %s", args, e, err.String())
	}
	return out.Bytes(), nil
}
func (r *Runpodctl) CreatePod(ctx context.Context, q PodRequest) (Pod, error) {
	args := []string{"pod", "create", "-o", "json", "--name", q.Name, "--image", q.Image, "--gpu-count", fmt.Sprint(q.GPUs), "--container-disk-in-gb", fmt.Sprint(q.DiskGB)}
	if q.TerminateAfter > 0 {
		args = append(args, "--terminate-after", time.Now().Add(time.Duration(q.TerminateAfter)*time.Second).UTC().Format(time.RFC3339))
	}
	if len(q.GPUIDs) > 0 {
		args = append(args, "--gpu-id", q.GPUIDs[0])
	}
	if q.PublicSSH {
		args = append(args, "--ssh")
	}
	b, e := r.call(ctx, args...)
	if e != nil {
		if p, le := r.find(ctx, q.Name); le == nil {
			return p, nil
		}
		return Pod{}, e
	}
	return parsePod(b)
}
func (r *Runpodctl) GetPod(ctx context.Context, id string) (Pod, error) {
	b, e := r.call(ctx, "pod", "get", "-o", "json", id)
	if e != nil {
		return Pod{}, e
	}
	return parsePod(b)
}
func (r *Runpodctl) ListPods(ctx context.Context, name string) ([]Pod, error) {
	b, e := r.call(ctx, "pod", "list", "-o", "json")
	if e != nil {
		return nil, e
	}
	var a []wirePod
	if json.Unmarshal(b, &a) != nil {
		var w struct {
			Pods []wirePod `json:"pods"`
		}
		if json.Unmarshal(b, &w) != nil {
			return nil, errors.New("runpodctl pod list: unsupported JSON")
		}
		a = w.Pods
	}
	var out []Pod
	for _, w := range a {
		if name == "" || w.Name == name {
			out = append(out, convert(w))
		}
	}
	return out, nil
}
func (r *Runpodctl) DeletePod(ctx context.Context, id string) error {
	_, e := r.call(ctx, "pod", "delete", id)
	return e
}
func (r *Runpodctl) StopPod(ctx context.Context, id string) error {
	_, e := r.call(ctx, "pod", "stop", id)
	return e
}
func (r *Runpodctl) SSHInfo(ctx context.Context, id string) (SSHInfo, error) {
	b, e := r.call(ctx, "ssh", "info", "-o", "json", id)
	if e != nil {
		return SSHInfo{}, e
	}
	var w struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		User       string `json:"user"`
		PrivateKey string `json:"privateKey"`
		KnownHosts string `json:"knownHosts"`
		SSHHost    string `json:"sshHost"`
		SSHPort    int    `json:"sshPort"`
	}
	if json.Unmarshal(b, &w) != nil {
		return SSHInfo{}, errors.New("runpodctl ssh info: unsupported JSON")
	}
	if w.Host == "" {
		w.Host = w.SSHHost
	}
	if w.Port == 0 {
		w.Port = w.SSHPort
	}
	if w.User == "" {
		w.User = "root"
	}
	if w.Host == "" || w.Port == 0 {
		return SSHInfo{}, errors.New("runpodctl ssh info: host and port required")
	}
	return SSHInfo{Host: w.Host, Port: w.Port, User: w.User, PrivateKey: w.PrivateKey, KnownHosts: w.KnownHosts}, nil
}
func (r *Runpodctl) find(ctx context.Context, name string) (Pod, error) {
	a, e := r.ListPods(ctx, name)
	if e != nil || len(a) != 1 {
		return Pod{}, errors.New("managed pod not uniquely found")
	}
	return a[0], nil
}
func parsePod(b []byte) (Pod, error) {
	var w wirePod
	if json.Unmarshal(b, &w) != nil || w.ID == "" {
		var wrap struct {
			Pod wirePod `json:"pod"`
		}
		if json.Unmarshal(b, &wrap) != nil || wrap.Pod.ID == "" {
			return Pod{}, errors.New("unsupported runpodctl pod JSON")
		}
		w = wrap.Pod
	}
	return convert(w), nil
}
func convert(w wirePod) Pod {
	id := w.ID
	if id == "" {
		id = w.Machine.PodID
	}
	host := w.SSHHost
	if host == "" {
		host = w.PublicIP
	}
	return Pod{ID: id, Name: w.Name, Status: w.Status, HourlyUSD: w.Cost, SSHHost: host, SSHPort: w.SSHPort}
}
func first(a []string) string {
	if len(a) > 0 {
		return a[0]
	}
	return ""
}

var _ = strings.TrimSpace
