package termux

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrefix(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Termux runs the Linux build")
	}
	for prefix, want := range map[string]string{
		"/data/data/com.termux/files/usr": "/data/data/com.termux/files/usr",
		"/usr":                            "",
		"":                                "",
	} {
		t.Setenv("PREFIX", prefix)
		if got := Prefix(); got != want {
			t.Errorf("Prefix() with PREFIX=%q = %q, want %q", prefix, got, want)
		}
	}
}

// Inside Termux, Go gets Termux's bundle. A user's own setting, and every
// system that is not Termux, stay as they are.
func TestUseCABundle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Termux runs the Linux build")
	}
	prefix := filepath.Join(t.TempDir(), "com.termux", "files", "usr")
	bundle := filepath.Join(prefix, "etc", "tls", "cert.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSL_CERT_DIR", "")
	for _, tt := range []struct {
		name, prefix, userFile, want string
	}{
		{name: "Termux", prefix: prefix, want: bundle},
		{name: "user's own bundle", prefix: prefix, userFile: "/mine.pem", want: "/mine.pem"},
		{name: "desktop Linux", prefix: "/usr", want: ""},
	} {
		t.Setenv("PREFIX", tt.prefix)
		t.Setenv("SSL_CERT_FILE", tt.userFile)
		UseCABundle()
		if got := os.Getenv("SSL_CERT_FILE"); got != tt.want {
			t.Errorf("%s: SSL_CERT_FILE = %q, want %q", tt.name, got, tt.want)
		}
	}
}
