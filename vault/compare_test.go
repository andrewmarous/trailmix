package vault

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestWriteIfUnchanged(t *testing.T) {
	v, _ := Open(t.TempDir())
	old, newBody := []byte("# Old\n"), []byte("# New\n")
	if err := v.WriteIfUnchanged("procedures/check", old, ""); err != nil {
		t.Fatal(err)
	}
	if err := v.WriteIfUnchanged("procedures/check", newBody, ""); err == nil {
		t.Fatal("replaced existing note without its baseline")
	}
	baseline := fmt.Sprintf("%x", sha256.Sum256(old))
	if err := v.WriteIfUnchanged("procedures/check", newBody, baseline); err != nil {
		t.Fatal(err)
	}
	if err := v.WriteIfUnchanged("procedures/check", newBody, baseline); err != nil {
		t.Fatal("identical retry failed:", err)
	}
	if err := v.WriteIfUnchanged("../outside", newBody, ""); err == nil {
		t.Fatal("unsafe path accepted")
	}
}
