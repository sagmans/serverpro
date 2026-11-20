//go:build !darwin

package credentials

// systemKeyringReady is nil off macOS: readiness is left to the Secret
// Service call itself, because on Linux a missing service fails fast over
// D-Bus without any dialog. A nil hook, not a no-op func, because
// keyringBackend already treats nil as ready, and a no-op that nothing calls
// fails the zero-coverage gate on Linux CI.
var systemKeyringReady func() error
