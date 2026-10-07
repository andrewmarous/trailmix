package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveLink(t *testing.T) {
	files := []string{"a/backoff.md", "b/backoff.md", "procedures/retry.md", "unique.md", "猫.md", "diagram.png", "UPPER.MD", "name.md", "name"}
	tests := []struct{ target, want, errorText string }{
		{"", "source.md", ""}, {"procedures/retry", "procedures/retry.md", ""}, {"procedures/retry.md", "procedures/retry.md", ""},
		{"unique", "unique.md", ""}, {"unique.md", "unique.md", ""}, {"猫", "猫.md", ""}, {"diagram.png", "diagram.png", ""}, {"UPPER", "UPPER.MD", ""},
		{"backoff", "", "ambiguous"}, {"backoff.md", "", "ambiguous"}, {"name", "", "ambiguous"},
		{"missing", "", "missing"}, {"Unique", "", "missing"}, {"a/missing", "", "missing"},
		{"../unique", "", "unsafe"}, {"a/../unique", "", "unsafe"}, {"/unique", "", "unsafe"}, {"./unique", "", "unsafe"},
		{".hidden", "", "unsafe"}, {"a/.hidden", "", "unsafe"}, {"a//backoff", "", "unsafe"}, {"a/", "", "unsafe"},
		{"a\\backoff", "", "unsafe"}, {"C:/unique", "", "unsafe"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got, err := resolveLink("source.md", tt.target, files)
			if tt.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errorText) {
					t.Fatalf("got %q, %v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestNavigation(t *testing.T) {
	v, _ := Open(t.TempDir())
	notes := map[string]string{
		"home.md":          "[[folder/target|Alias]] [[folder/target#Heading]] ![[image.png]] [[#Local]]\n[[folder/target#^block]]",
		"folder/target.md": "[[home]] [[home.md]] `[[missing]]`",
		"other.md":         "[[folder/target]]",
	}
	for name, body := range notes {
		if err := v.Write(name, []byte(body), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(v.Root, "image.png"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := v.Navigate("home")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Outgoing) != 5 || len(got.Backlinks) != 3 {
		t.Fatalf("%+v", got)
	}
	if got.Outgoing[0].Path != "folder/target.md" || got.Outgoing[0].Alias != "Alias" || got.Outgoing[1].Fragment != "Heading" || !got.Outgoing[2].Embed || got.Outgoing[3].Path != "home.md" || got.Outgoing[4].Fragment != "^block" {
		t.Fatalf("%+v", got.Outgoing)
	}
	sources := []string{}
	for _, ref := range got.Backlinks {
		sources = append(sources, ref.Source)
	}
	if !reflect.DeepEqual(sources, []string{"folder/target.md", "folder/target.md", "home.md"}) {
		t.Fatal(sources)
	}
}

func TestNavigationFailures(t *testing.T) {
	for _, target := range []string{"missing", "duplicate", "../outside", "hidden", "linked"} {
		t.Run(target, func(t *testing.T) {
			v, _ := Open(t.TempDir())
			for name, body := range map[string]string{"home": "ok", "other": "[[" + target + "]]", "a/duplicate": "", "b/duplicate": ""} {
				if err := v.Write(name, []byte(body), false); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(v.Root, ".hidden"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(v.Root, ".hidden", "hidden.md"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(v.Root, "home.md"), filepath.Join(v.Root, "linked.md")); err != nil {
				t.Fatal(err)
			}
			got, err := v.Navigate("home")
			if got != nil || err == nil || !strings.Contains(err.Error(), "other.md:1:") {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestNavigationEmptyAndMissing(t *testing.T) {
	v, _ := Open(t.TempDir())
	if _, err := v.Navigate("missing"); err == nil {
		t.Fatal("missing accepted")
	}
	if err := v.Write("empty", nil, false); err != nil {
		t.Fatal(err)
	}
	got, err := v.Navigate("empty")
	if err != nil || got.Outgoing == nil || got.Backlinks == nil || len(got.Outgoing)+len(got.Backlinks) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}
