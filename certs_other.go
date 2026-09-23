//go:build !linux || android

package main

// useAndroidCerts does nothing outside the Linux build. The Android build
// finds Android's CA certificates itself, and other systems have their own
// stores. See certs_linux.go.
func useAndroidCerts() {}
