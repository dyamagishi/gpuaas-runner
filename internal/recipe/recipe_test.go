package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDigestAndPlaceholder(t *testing.T) {
	r := Recipe{SchemaVersion: 1, Name: "x", Image: "ghcr.io/x@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Inputs: map[string]Input{"data": {Type: "string", Required: true}}, Requirements: Requirements{GPUs: 1, MinVRAMGB: 1, DiskGB: 1, CUDA: "12.4"}, Stages: []Stage{{ID: "s", Argv: []string{"echo", "${inputs.data}", "${run.id}"}}}}
	if e := Validate(r); e != nil {
		t.Fatal(e)
	}
	if _, e := Expand("${inputs.data}/${run.id}", map[string]string{"data": "ok"}, "r1"); e != nil {
		t.Fatal(e)
	}
}
func TestBundledRecipeLoadsWithPinnedImage(t *testing.T) {
	b, e := os.ReadFile("../../recipes/sdxl-lora.yaml")
	if e != nil {
		t.Skip(e)
	}
	s := strings.Replace(string(b), "REPLACE_WITH_64_HEX_DIGEST", strings.Repeat("a", 64), 1)
	p := filepath.Join(t.TempDir(), "r.yaml")
	if e = os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(p); e != nil {
		t.Fatal(e)
	}
}
func TestRejectsUnsafePath(t *testing.T) {
	r := Recipe{SchemaVersion: 1, Name: "x", Image: "ghcr.io/x@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Stages: []Stage{{ID: "s", Argv: []string{"echo"}}}, Artifacts: []Artifact{{Name: "x", RemotePath: "../x"}}}
	if Validate(r) == nil {
		t.Fatal("expected path rejection")
	}
}
func TestResolveTyped(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x")
	if e := os.WriteFile(p, []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	r := Recipe{SchemaVersion: 1, Name: "x", Image: "ghcr.io/x@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Inputs: map[string]Input{"n": {Type: "integer", Required: true}, "f": {Type: "file", Required: true}}, Stages: []Stage{{ID: "s", Argv: []string{"echo"}}}}
	if _, e := ResolveInputs(r, map[string]string{"n": "2", "f": p}); e != nil {
		t.Fatal(e)
	}
}

func TestResolveDirectory(t *testing.T) {
	d := t.TempDir()
	r := Recipe{Inputs: map[string]Input{"data": {Type: "directory", Required: true}}}
	if _, e := ResolveInputs(r, map[string]string{"data": d}); e != nil {
		t.Fatal(e)
	}
}
