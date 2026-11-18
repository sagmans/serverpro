//go:build !darwin

package credentials

// systemKeyringReady leaves readiness to the Secret Service call itself:
// on Linux a missing service fails fast over D-Bus without any dialog.
func systemKeyringReady() error { return nil }
