package provider

import (
	"testing"
)

func TestParsePodShapes(t *testing.T) {
	for _, b := range [][]byte{[]byte(`{"id":"p1","name":"r","status":"RUNNING","sshHost":"1.2.3.4","sshPort":22}`), []byte(`{"pod":{"id":"p1","name":"r"}}`)} {
		p, e := parsePod(b)
		if e != nil || p.ID != "p1" {
			t.Fatalf("%v %#v", e, p)
		}
	}
}
