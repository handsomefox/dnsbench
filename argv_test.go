package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDropLinkerArg(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "dnsbench")
	other := filepath.Join(dir, "other", "dnsbench")
	for _, path := range []string{self, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "Termux inserts the path", args: []string{"./dnsbench", self, "-ui"}, want: []string{"./dnsbench", "-ui"}},
		{name: "path alone", args: []string{"./dnsbench", self}, want: []string{"./dnsbench"}},
		{name: "plain flags", args: []string{"./dnsbench", "-ui"}, want: []string{"./dnsbench", "-ui"}},
		{name: "no arguments", args: []string{"./dnsbench"}, want: []string{"./dnsbench"}},
		{name: "relative path", args: []string{"./dnsbench", "dnsbench", "-ui"}, want: []string{"./dnsbench", "dnsbench", "-ui"}},
		{name: "another file with the same name", args: []string{"./dnsbench", other, "-ui"}, want: []string{"./dnsbench", other, "-ui"}},
	}
	for _, tt := range tests {
		if got := dropLinkerArg(slices.Clone(tt.args)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: dropLinkerArg(%q) = %q, want %q", tt.name, tt.args, got, tt.want)
		}
	}
}
