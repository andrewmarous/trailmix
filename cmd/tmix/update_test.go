package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdateCLI(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		fail bool
	}{
		{[]string{"update", "--help"}, "check the latest stable release", false},
		{[]string{"update", "extra"}, "update takes no positional arguments", true},
		{[]string{"update", "--unknown"}, "flag provided but not defined", true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			err := run(append([]string{"--config", t.TempDir() + "/missing.toml"}, tc.args...), strings.NewReader(""), &out, &diagnostics)
			got := out.String() + diagnostics.String()
			if err != nil {
				got += err.Error()
			}
			if (err != nil) != tc.fail || !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}
