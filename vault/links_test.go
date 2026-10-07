package vault

import (
	"reflect"
	"testing"
)

func TestParseLinks(t *testing.T) {
	tests := []struct {
		name, input string
		want        []Link
	}{
		{"forms", "[[procedures/backoff]] [[backoff|Retry delays]]\n![[backoff#Limits]] [[#Local]] [[note#^block]]", []Link{
			{Target: "procedures/backoff", Line: 1}, {Target: "backoff", Alias: "Retry delays", Line: 1},
			{Target: "backoff", Fragment: "Limits", Embed: true, Line: 2}, {Fragment: "Local", Line: 2}, {Target: "note", Fragment: "^block", Line: 2},
		}},
		{"code", "`[[hidden]]` ``[[hidden]] ` `` [[visible]]\n```md\n[[hidden]]\n```\n~~~\n[[hidden]]\n~~~\n[[end]]", []Link{{Target: "visible", Line: 1}, {Target: "end", Line: 8}}},
		{"comments", "<!-- [[hidden]]\n[[hidden]] --> [[visible]]", []Link{{Target: "visible", Line: 2}}},
		{"escapes", "\\[[hidden]] \\![[visible]] \\\\[[also-visible]]", []Link{{Target: "visible", Line: 1}, {Target: "also-visible", Line: 1}}},
		{"unmatched tick", "` [[visible]]", []Link{{Target: "visible", Line: 1}}},
		{"multiline code", "`code\n[[hidden]]` [[visible]]", []Link{{Target: "visible", Line: 2}}},
		{"malformed", "[[]] [[unfinished\n[[valid]]", []Link{{Target: "valid", Line: 2}}},
		{"unicode duplicates", "[[猫]] [[猫]]", []Link{{Target: "猫", Line: 1}, {Target: "猫", Line: 1}}},
		{"empty", "plain text", []Link{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLinks(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestVaultLinks(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Write("example", []byte("[[other]]"), false); err != nil {
		t.Fatal(err)
	}
	got, err := v.Links("example")
	if err != nil || len(got) != 1 || got[0].Target != "other" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := v.Links("missing"); err == nil {
		t.Fatal("missing note accepted")
	}
}
