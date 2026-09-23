package main

import (
	"os"
	"runtime"
)

// androidCertDir holds Android's system CA certificates.
const androidCertDir = "/system/etc/security/cacerts"

// useAndroidCerts points the Linux build at Android's CA certificates when
// it runs on Android, as in Termux. Go reads them itself only when built
// for Android, and none of the Linux paths it tries exist there, so every
// DoT, DoH, and DoQ precheck would fail. It changes nothing when the user
// set SSL_CERT_FILE or SSL_CERT_DIR, or when a Linux bundle exists.
func useAndroidCerts() {
	// An Android build (GOOS android) finds these certificates itself, and
	// other systems have their own stores.
	if runtime.GOOS != "linux" {
		return
	}
	if os.Getenv("SSL_CERT_FILE") != "" || os.Getenv("SSL_CERT_DIR") != "" {
		return
	}
	for _, f := range []string{"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/cert.pem"} {
		if _, err := os.Stat(f); err == nil {
			return
		}
	}
	if info, err := os.Stat(androidCertDir); err == nil && info.IsDir() {
		// Setenv fails only for an invalid name, which this is not.
		_ = os.Setenv("SSL_CERT_DIR", androidCertDir) //nolint:errcheck // see above
	}
}
