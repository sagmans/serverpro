package config

import (
	"errors"
	"fmt"
	"time"
)

// Reboot sets the gated reboot that finishes kernel and library updates.
// Unattended upgrades install them but never reboot on their own, because a
// blind reboot can drop a workload that was started by hand; the gate reboots
// only when every running workload is declared to come back.
type Reboot struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Window is the daily start time, HH:MM in UTC, so one config value means
	// the same moment on hosts in any provider region.
	Window string `yaml:"window,omitempty" json:"window,omitempty"`
	// VerifyTimeout bounds the post-boot wait for workloads, as a Go duration
	// string, so a slow image pull cannot keep the check running forever.
	VerifyTimeout string `yaml:"verify_timeout,omitempty" json:"verify_timeout,omitempty"`
}

const (
	// rebootWindowLayout is a 24-hour clock, so a window never depends on an
	// AM/PM reading and maps to one systemd OnCalendar time.
	rebootWindowLayout = "15:04"
	// The verify timeout must give a booted host time to start its workloads,
	// and must end before an operator would miss a stuck workload until the
	// next working day.
	minRebootVerifyTimeout = time.Minute
	maxRebootVerifyTimeout = time.Hour
)

// WindowClock returns the window's hour and minute. Validate rejects a window
// it cannot parse, so callers that run after Validate get a real time.
func (r Reboot) WindowClock() (hour, minute int, err error) {
	at, err := time.Parse(rebootWindowLayout, r.Window)
	if err != nil {
		return 0, 0, fmt.Errorf("reboot.window must be HH:MM in UTC: %w", err)
	}
	return at.Hour(), at.Minute(), nil
}

// VerifyTimeoutDuration parses the verify timeout and keeps it inside the
// bounds, so the host-side wait and the unit's runtime limit stay finite.
func (r Reboot) VerifyTimeoutDuration() (time.Duration, error) {
	timeout, err := time.ParseDuration(r.VerifyTimeout)
	if err != nil {
		return 0, fmt.Errorf("reboot.verify_timeout must be a duration such as %s: %w", DefaultRebootVerifyTimeout, err)
	}
	if timeout < minRebootVerifyTimeout || timeout > maxRebootVerifyTimeout {
		return 0, fmt.Errorf("reboot.verify_timeout must be between %s and %s", minRebootVerifyTimeout, maxRebootVerifyTimeout)
	}
	if timeout%time.Second != 0 {
		return 0, errors.New("reboot.verify_timeout must be whole seconds")
	}
	return timeout, nil
}

// validateReboot checks the values even when the gate is off, so turning it
// on later cannot fail on a value that was saved long before.
func (c Config) validateReboot() error {
	if _, _, err := c.Reboot.WindowClock(); err != nil {
		return err
	}
	_, err := c.Reboot.VerifyTimeoutDuration()
	return err
}
