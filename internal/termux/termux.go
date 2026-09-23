// Package termux adapts dnsbench to Termux, the Linux environment for
// Android. dnsbench runs there as the static linux/arm64 build, since Go
// supports Android programs only when built with cgo.
package termux

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Prefix returns Termux's install prefix when dnsbench runs inside
// Termux, or "" anywhere else. Termux sets PREFIX to a directory in its
// app's data, such as /data/data/com.termux/files/usr, which no desktop or
// server Linux uses.
func Prefix() string {
	prefix := os.Getenv("PREFIX")
	if runtime.GOOS != "linux" || !strings.Contains(prefix, "/com.termux/") {
		return ""
	}
	return prefix
}

// UseCABundle points Go at Termux's CA bundle. The Linux build looks for
// certificates only in the paths desktop distributions use, and none exist
// on Android, so every DoT, DoH, and DoQ resolver would fail its
// certificate check. Outside Termux, or when SSL_CERT_FILE or SSL_CERT_DIR
// is set, it does nothing.
func UseCABundle() {
	prefix := Prefix()
	if prefix == "" || os.Getenv("SSL_CERT_FILE") != "" || os.Getenv("SSL_CERT_DIR") != "" {
		return
	}
	bundle := filepath.Join(prefix, "etc", "tls", "cert.pem")
	if _, err := os.Stat(bundle); err == nil {
		// Setenv fails only for an invalid name, which this is not.
		_ = os.Setenv("SSL_CERT_FILE", bundle) //nolint:errcheck // see above
	}
}
