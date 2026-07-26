package runner

import (
	"testing"
)

func TestSafeRemote(t *testing.T) {
	if safeRemote("../x") == nil || safeRemote("/x") == nil {
		t.Fatal("unsafe path accepted")
	}
	if safeRemote("runs/x/status.json") != nil {
		t.Fatal("safe path rejected")
	}
}
func TestShellQuote(t *testing.T) {
	if shellQuote("a'b") != "'a'\\''b'" {
		t.Fatal(shellQuote("a'b"))
	}
}
