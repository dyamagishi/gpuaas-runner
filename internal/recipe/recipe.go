package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Recipe struct {
	SchemaVersion int              `yaml:"schema_version"`
	Name          string           `yaml:"name"`
	Image         string           `yaml:"image"`
	Inputs        map[string]Input `yaml:"inputs"`
	Requirements  Requirements     `yaml:"requirements"`
	Transfers     []Transfer       `yaml:"transfers"`
	Stages        []Stage          `yaml:"stages"`
	Artifacts     []Artifact       `yaml:"artifacts"`
}
type Input struct {
	Type       string   `yaml:"type"`
	Required   bool     `yaml:"required"`
	Default    any      `yaml:"default"`
	Allowed    []string `yaml:"allowed"`
	Enum       []string `yaml:"enum"`
	Extensions []string `yaml:"extensions"`
	Secret     bool     `yaml:"secret"`
}
type Requirements struct {
	GPUCount   int      `yaml:"gpu_count"`
	GPUs       int      `yaml:"gpus"`
	MinVRAMGB  int      `yaml:"min_vram_gb"`
	AllowedGPU []string `yaml:"allowed_gpu_ids"`
	DiskGB     int      `yaml:"disk_gb"`
	CUDA       string   `yaml:"cuda"`
	Env        []string `yaml:"env"`
}
type Transfer struct {
	Input      string `yaml:"input"`
	RemotePath string `yaml:"remote_path"`
}
type Stage struct {
	ID         string   `yaml:"id"`
	Argv       []string `yaml:"argv"`
	WorkingDir string   `yaml:"working_dir"`
	EnvFrom    []string `yaml:"env_from"`
	Timeout    string   `yaml:"timeout"`
}
type Artifact struct {
	Name       string `yaml:"name"`
	Kind       string `yaml:"kind"`
	RemotePath string `yaml:"remote_path"`
	Required   bool   `yaml:"required"`
	When       string `yaml:"when"`
	SHA256     string `yaml:"sha256"`
}

var digestRE = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
var hfRefRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-fA-F]{40}$`)
var refRE = regexp.MustCompile(`\$\{(?:(?:inputs|transfers)\.([A-Za-z_][A-Za-z0-9_]*)|run\.id)\}`)

func Load(path string) (Recipe, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Recipe{}, e
	}
	var n yaml.Node
	if e = yaml.Unmarshal(b, &n); e != nil {
		return Recipe{}, e
	}
	if e = duplicateCheck(&n); e != nil {
		return Recipe{}, e
	}
	var r Recipe
	d := yaml.NewDecoder(strings.NewReader(string(b)))
	d.KnownFields(true)
	if e = d.Decode(&r); e != nil {
		return r, e
	}
	return r, Validate(r)
}
func duplicateCheck(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if seen[k] {
				return fmt.Errorf("duplicate field %q", k)
			}
			seen[k] = true
			if e := duplicateCheck(n.Content[i+1]); e != nil {
				return e
			}
		}
	}
	for _, c := range n.Content {
		if e := duplicateCheck(c); e != nil {
			return e
		}
	}
	return nil
}
func Validate(r Recipe) error {
	if r.SchemaVersion != 1 || r.Name == "" {
		return errors.New("schema_version 1 and name are required")
	}
	if !digestRE.MatchString(r.Image) {
		return errors.New("image must end with @sha256:<64 lowercase hex>")
	}
	if len(r.Stages) == 0 {
		return errors.New("stages required")
	}
	if r.Requirements.GPUs <= 0 && r.Requirements.GPUCount <= 0 {
		return errors.New("requirements.gpus must be positive")
	}
	if r.Requirements.DiskGB <= 0 || r.Requirements.MinVRAMGB <= 0 || r.Requirements.CUDA == "" {
		return errors.New("requirements disk_gb, min_vram_gb, and cuda are required")
	}
	for name, in := range r.Inputs {
		switch in.Type {
		case "string", "integer", "number", "boolean", "enum", "file", "directory", "https_url", "hf_ref":
		default:
			return fmt.Errorf("input %s has invalid type", name)
		}
	}
	transfers := map[string]bool{}
	for _, t := range r.Transfers {
		if _, ok := r.Inputs[t.Input]; !ok {
			return fmt.Errorf("transfer references unknown input %s", t.Input)
		}
		if unsafePath(t.RemotePath) {
			return fmt.Errorf("unsafe remote path %s", t.RemotePath)
		}
		transfers[t.Input] = true
	}
	for _, s := range r.Stages {
		if s.ID == "" || len(s.Argv) == 0 {
			return errors.New("stage id and argv required")
		}
		for _, a := range s.Argv {
			if e := validateTemplate(a, r.Inputs, transfers); e != nil {
				return e
			}
		}
		if e := validateTemplate(s.WorkingDir, r.Inputs, transfers); e != nil {
			return e
		}
	}
	for _, a := range r.Artifacts {
		if unsafePath(a.RemotePath) {
			return fmt.Errorf("unsafe artifact path %s", a.RemotePath)
		}
		if e := validateTemplate(a.RemotePath, r.Inputs, transfers); e != nil {
			return e
		}
	}
	return nil
}
func validateTemplate(s string, inputs map[string]Input, transfers map[string]bool) error {
	for _, m := range refRE.FindAllStringSubmatch(s, -1) {
		if m[1] != "" {
			if _, ok := inputs[m[1]]; !ok && !transfers[m[1]] {
				return fmt.Errorf("unresolved input reference %s", m[1])
			}
		}
	}
	rest := refRE.ReplaceAllString(s, "")
	if strings.ContainsAny(rest, ";&|<>`$()") || strings.Contains(rest, "${") {
		return fmt.Errorf("unsafe shell/template expression %q", s)
	}
	return nil
}
func Expand(s string, vals map[string]string, runID string) (string, error) {
	return ExpandWithTransfers(s, vals, nil, runID)
}
func ExpandWithTransfers(s string, vals, transfers map[string]string, runID string) (string, error) {
	var missing string
	out := refRE.ReplaceAllStringFunc(s, func(x string) string {
		m := refRE.FindStringSubmatch(x)
		if strings.HasPrefix(x, "${transfers.") {
			v, ok := transfers[m[1]]
			if !ok {
				missing = m[1]
			}
			return v
		}
		if m[1] != "" {
			v, ok := vals[m[1]]
			if !ok {
				missing = m[1]
			}
			return v
		}
		return runID
	})
	if missing != "" {
		return "", fmt.Errorf("input value missing for placeholder %s", missing)
	}
	if strings.Contains(out, "${") {
		return "", errors.New("unresolved placeholder")
	}
	return out, nil
}
func ResolveInputs(r Recipe, values map[string]string) (map[string]any, error) {
	for n := range values {
		if _, ok := r.Inputs[n]; !ok {
			return nil, fmt.Errorf("unknown input %s", n)
		}
	}
	out := map[string]any{}
	for n, in := range r.Inputs {
		v, ok := values[n]
		if !ok && in.Default != nil {
			v = fmt.Sprint(in.Default)
			ok = true
		}
		if in.Required && !ok {
			return nil, fmt.Errorf("missing input %s", n)
		}
		if !ok {
			continue
		}
		switch in.Type {
		case "file", "directory":
			if e := validatePath(v, in.Type == "directory"); e != nil {
				return nil, fmt.Errorf("input %s: %w", n, e)
			}
			if len(in.Extensions) > 0 {
				if e := validateExtensions(v, in.Extensions); e != nil {
					return nil, fmt.Errorf("input %s: %w", n, e)
				}
			}
		case "https_url":
			u, e := url.Parse(v)
			if e != nil || u.Scheme != "https" || u.Host == "" {
				return nil, fmt.Errorf("input %s must be https URL", n)
			}
		case "enum":
			allowed := in.Enum
			if len(allowed) == 0 {
				allowed = in.Allowed
			}
			found := false
			for _, x := range allowed {
				if x == v {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("input %s not allowed", n)
			}
		case "integer":
			if _, e := strconv.Atoi(v); e != nil {
				return nil, fmt.Errorf("input %s integer required", n)
			}
		case "number":
			if _, e := strconv.ParseFloat(v, 64); e != nil {
				return nil, fmt.Errorf("input %s number required", n)
			}
		case "boolean":
			if _, e := strconv.ParseBool(v); e != nil {
				return nil, fmt.Errorf("input %s boolean required", n)
			}
		case "hf_ref":
			if !hfRefRE.MatchString(v) {
				return nil, fmt.Errorf("input %s must be repo@40-hex-commit", n)
			}
		}
		out[n] = v
	}
	return out, nil
}
func unsafePath(p string) bool {
	return filepath.IsAbs(p) || p == "" || strings.Contains(filepath.Clean(p), "..")
}
func validatePath(p string, dir bool) error {
	i, e := os.Lstat(p)
	if e != nil {
		return e
	}
	if i.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink rejected")
	}
	if dir && !i.IsDir() {
		return errors.New("directory required")
	}
	if !dir && !i.Mode().IsRegular() {
		return errors.New("regular file required")
	}
	if !i.IsDir() && i.Mode()&fs.ModeType != 0 {
		return errors.New("special file rejected")
	}
	return nil
}

func validateExtensions(root string, extensions []string) error {
	allowed := map[string]bool{}
	for _, ext := range extensions {
		allowed[strings.ToLower(strings.TrimPrefix(ext, "."))] = true
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink rejected: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("special file rejected: %s", path)
		}
		if !allowed[strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")] {
			return fmt.Errorf("extension not allowed: %s", path)
		}
		return nil
	})
}
func Digest(r Recipe) string {
	b, _ := yaml.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
