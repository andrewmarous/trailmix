package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/vault"
)

func TestLinksCLI(t *testing.T) {
	root := t.TempDir()
	v, _ := vault.Open(root)
	for name, body := range map[string]string{"a": "[[b]]", "b": "[[a]]"} {
		if err := v.Write(name, []byte(body), false); err != nil {
			t.Fatal(err)
		}
	}
	var out, diagnostics bytes.Buffer
	invoke := func(args ...string) error {
		out.Reset()
		diagnostics.Reset()
		return run(append([]string{"--vault", root, "links"}, args...), strings.NewReader(""), &out, &diagnostics)
	}
	if err := invoke("--json", "a"); err != nil {
		t.Fatal(err)
	}
	var nav vault.Navigation
	if err := json.Unmarshal(out.Bytes(), &nav); err != nil {
		t.Fatal(err)
	}
	if len(nav.Outgoing) != 1 || nav.Outgoing[0].Path != "b.md" || len(nav.Backlinks) != 1 {
		t.Fatalf("%+v", nav)
	}
	if err := invoke("a"); err != nil || out.String() != "outgoing a.md:1: b.md\nbacklink b.md:1: a.md\n" {
		t.Fatalf("%q %v", out.String(), err)
	}
	for _, args := range [][]string{nil, {"a", "b"}, {"missing"}} {
		if err := invoke(args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := v.Write("broken", []byte("[[missing]]"), false); err != nil {
		t.Fatal(err)
	}
	if err := invoke("--json", "a"); err == nil || out.Len() != 0 {
		t.Fatalf("partial output %q, error %v", out.String(), err)
	}
}
