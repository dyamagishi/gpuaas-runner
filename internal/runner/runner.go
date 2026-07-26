package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type SSH struct {
	Host                  string
	Port                  int
	User, Key, KnownHosts string
}
type Artifact struct {
	Name, RemotePath, Kind string
	Required               bool
}
type Job struct {
	RunID, RemoteDir string
	SSH              SSH
	Stages           [][]string
	WorkingDirs      []string
	ArtifactRoot     string
	Artifacts        []Artifact
}

func (q Job) validateSSH() error {
	if q.SSH.Host == "" || q.SSH.User == "" || q.SSH.Port < 1 || q.SSH.Port > 65535 || q.SSH.Key == "" {
		return errors.New("incomplete ssh configuration")
	}
	if strings.ContainsAny(q.SSH.Host, " \t\n\r:/") || strings.ContainsAny(q.SSH.User, " \t\n\r:@/") {
		return errors.New("unsafe ssh host or user")
	}
	for _, s := range []string{q.SSH.Host, q.SSH.User, q.SSH.Key, q.SSH.KnownHosts} {
		if strings.ContainsAny(s, "\x00\r\n;&|<>`$()") {
			return errors.New("unsafe ssh configuration")
		}
	}
	if q.RemoteDir == "" || !filepath.IsAbs(q.RemoteDir) || strings.Contains(filepath.Clean(q.RemoteDir), "..") || strings.ContainsAny(q.RemoteDir, "\x00;&|<>`$") {
		return errors.New("unsafe remote directory")
	}
	return nil
}

func safeRemote(p string) error {
	if p == "" || filepath.IsAbs(p) || strings.Contains(filepath.Clean(p), "..") || strings.ContainsAny(p, ";&|<>`$()") {
		return errors.New("unsafe remote path")
	}
	return nil
}

func (q Job) sshArgs() []string {
	checking := "accept-new"
	if q.SSH.KnownHosts != "" {
		checking = "yes"
	}
	a := []string{"-p", fmt.Sprint(q.SSH.Port), "-i", q.SSH.Key, "-o", "StrictHostKeyChecking=" + checking}
	if q.SSH.KnownHosts != "" {
		a = append(a, "-o", "UserKnownHostsFile="+q.SSH.KnownHosts)
	}
	return a
}
func (q Job) sshTarget(p string) string { return fmt.Sprintf("%s@%s:%s", q.SSH.User, q.SSH.Host, p) }
func (q Job) sshHostTarget() string     { return fmt.Sprintf("%s@%s", q.SSH.User, q.SSH.Host) }
func (q Job) rsyncSSH() string {
	parts := make([]string, 0, len(q.sshArgs())+1)
	parts = append(parts, "ssh")
	for _, arg := range q.sshArgs() {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func (q Job) Transfer(ctx context.Context, src, dst string) error {
	if err := q.validateSSH(); err != nil {
		return err
	}
	if err := safeRemote(dst); err != nil {
		return err
	}
	return q.exec(ctx, "rsync", "-az", "-e", q.rsyncSSH(), src, q.sshTarget(filepath.Join(q.RemoteDir, dst)))
}

// Run writes each stage's argv as a NUL-delimited file and asks the image's
// remote-runner to execute it. No shell evaluation of user supplied argv occurs.
func (q Job) Run(ctx context.Context) error {
	if err := q.validateSSH(); err != nil {
		return err
	}
	if q.RemoteDir == "" || q.RunID == "" {
		return errors.New("run id and remote dir required")
	}
	if err := q.exec(ctx, "ssh", append(append(q.sshArgs(), q.sshHostTarget(), "--", "mkdir", "-p", q.RemoteDir), []string{}...)...); err != nil {
		return err
	}
	for i, argv := range q.Stages {
		if len(argv) == 0 || argv[0] == "" {
			return errors.New("empty stage")
		}
		f, err := os.CreateTemp("", "gpu-run-argv-")
		if err != nil {
			return err
		}
		name := f.Name()
		defer os.Remove(name)
		for _, v := range argv {
			if _, err = f.Write(append([]byte(v), 0)); err != nil {
				f.Close()
				return err
			}
		}
		if err = f.Close(); err != nil {
			return err
		}
		remoteArgv := fmt.Sprintf("%s/stage-%d.argv", q.RemoteDir, i)
		if err = q.exec(ctx, "rsync", "-az", "-e", q.rsyncSSH(), name, q.sshTarget(remoteArgv)); err != nil {
			return err
		}
		wd := "/workspace"
		if i < len(q.WorkingDirs) && q.WorkingDirs[i] != "" {
			wd = q.WorkingDirs[i]
		}
		root := q.ArtifactRoot
		if root == "" {
			root = q.RemoteDir
		}
		remote := []string{"/opt/gpu-run/remote-runner", "start", q.RemoteDir, remoteArgv, wd, root}
		if err = q.exec(ctx, "ssh", append(append(q.sshArgs(), q.sshHostTarget(), "--"), remote...)...); err != nil {
			return err
		}
	}
	return nil
}

func (q Job) Poll(ctx context.Context, timeout time.Duration) (map[string]any, error) {
	if err := q.validateSSH(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = time.Hour
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, err := q.capture(ctx, "ssh", append(append(q.sshArgs(), q.sshHostTarget(), "--", "cat", filepath.Join(q.RemoteDir, "status.json")), []string{}...)...)
		if err == nil {
			var v map[string]any
			if json.Unmarshal(b, &v) == nil {
				phase, _ := v["phase"].(string)
				code, _ := v["exit_code"].(float64)
				if phase == "completed" || phase == "failed" || v["done"] == true {
					if phase != "completed" || code != 0 {
						return v, fmt.Errorf("remote job failed (phase=%s exit_code=%v)", phase, code)
					}
					return v, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, errors.New("remote job timeout")
}

func (q Job) FetchArtifacts(ctx context.Context, out string) error {
	if err := q.validateSSH(); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		return err
	}
	for _, a := range q.Artifacts {
		if a.Name == "" || filepath.IsAbs(a.Name) || strings.Contains(filepath.Clean(a.Name), "..") || strings.ContainsAny(a.Name, "/\\\x00") {
			return errors.New("unsafe artifact name")
		}
		if err := safeRemote(a.RemotePath); err != nil {
			return err
		}
		var tmpPath string
		var err error
		if a.Kind == "directory" {
			tmpPath, err = os.MkdirTemp(out, ".artifact-")
		} else {
			var tmp *os.File
			tmp, err = os.CreateTemp(out, ".artifact-")
			if err == nil {
				tmpPath = tmp.Name()
				tmp.Close()
				_ = os.Remove(tmpPath)
			}
		}
		if err != nil {
			return err
		}
		source := q.sshTarget(filepath.Join(q.RemoteDir, a.RemotePath))
		dest := tmpPath
		if a.Kind == "directory" {
			source += "/"
			dest += "/"
		}
		if err := q.exec(ctx, "rsync", "-az", "-e", q.rsyncSSH(), source, dest); err != nil {
			_ = os.RemoveAll(tmpPath)
			if a.Required {
				return err
			}
			continue
		}
		if err := os.Rename(tmpPath, filepath.Join(out, a.Name)); err != nil {
			return err
		}
	}
	return nil
}

func (q Job) exec(ctx context.Context, name string, args ...string) error {
	_, err := q.capture(ctx, name, args...)
	return err
}
func (q Job) capture(ctx context.Context, name string, args ...string) ([]byte, error) {
	x := exec.CommandContext(ctx, name, args...)
	return x.Output()
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
