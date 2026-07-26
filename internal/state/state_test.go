package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "runs.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	now := time.Now()
	if e = s.Put(Run{ID: "r", Recipe: "x", Phase: "queued", CreatedAt: now, UpdatedAt: now, OutputDir: "out"}); e != nil {
		t.Fatal(e)
	}
	r, e := s.Get("r")
	if e != nil || r.OutputDir != "out" {
		t.Fatalf("%v %#v", e, r)
	}
}
