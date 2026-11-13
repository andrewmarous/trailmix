package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRetrievalConfigurationExpandsLocalArtifactPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	input := []byte("[retrieval]\nmode = \"text\"\nruntime = \"~/bin/llama-server\"\nmodel = \"~/models/qwen.gguf\"\nstate_dir = \"~/cache/retrieval\"\n")
	if err := os.WriteFile(path, input, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "/home/test")
	want := Retrieval{Mode: "text", Runtime: "/home/test/bin/llama-server", Model: "/home/test/models/qwen.gguf", StateDir: "/home/test/cache/retrieval"}
	if err != nil || !reflect.DeepEqual(c.Retrieval, want) {
		t.Fatalf("retrieval configuration %+v: %v", c.Retrieval, err)
	}
	if err = os.WriteFile(path, []byte("[retrieval]\nmodel = \"relative.gguf\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path, "/home/test"); err == nil || !strings.Contains(err.Error(), "retrieval model and state_dir must be absolute paths") {
		t.Fatalf("relative artifact path: %v", err)
	}
}
