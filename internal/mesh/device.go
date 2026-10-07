package mesh

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Selection errors split into one retryable outcome and terminal identity
// failures. Callers that poll for a booting node keep waiting only on
// ErrDeviceNotFound; every other error means privileged traffic must not be
// sent to any candidate.
var (
	// ErrDeviceNotFound means no device satisfies the identity yet. A booting
	// node may still enrol, so pollers retry.
	ErrDeviceNotFound = errors.New("mesh device not found")
	// ErrDeviceAmbiguous means more than one device satisfies the identity, so
	// no candidate can be trusted as the managed node.
	ErrDeviceAmbiguous = errors.New("mesh device identity is ambiguous")
	// ErrBoundDeviceMissing means the device recorded in state no longer exists.
	// Falling back to name matching would let another device take its place.
	ErrBoundDeviceMissing = errors.New("recorded mesh device not found")
	// ErrBoundDeviceMismatch means the recorded device ID now carries a
	// different name or tag set than the managed identity.
	ErrBoundDeviceMismatch = errors.New("recorded mesh device identity changed")
)

// DeviceQuery describes the managed node identity for SelectDevice. NodeID
// takes precedence: when it is set, CreatedNotBefore is ignored. Build queries
// for a managed server with ManagedDeviceQuery so every flow applies the same
// precedence.
type DeviceQuery struct {
	// Hostname is the expected short name or MagicDNS name.
	Hostname string
	// Tags must all be present on the device.
	Tags []string
	// NodeID binds selection to a device recorded in state. When set, name and
	// tags are verified against that one device instead of searched for.
	NodeID string
	// CreatedNotBefore drops devices enrolled before the single-use bootstrap
	// key existed, so stale devices that share the hostname cannot satisfy it.
	// It is the key's control-plane creation time when the API returned one,
	// else the local mint time widened by a clock-skew margin. The zero value
	// disables the filter.
	CreatedNotBefore time.Time
}

// ManagedDeviceQuery builds the identity query for a managed server from what
// state recorded. An open enrolment window means a key was minted for new
// compute that has not bound yet, so any recorded device belongs to an earlier
// server and must not be reused; otherwise the recorded device is verified.
func ManagedDeviceQuery(hostname string, tags []string, recordedNodeID string, enrolledSince time.Time) DeviceQuery {
	q := DeviceQuery{Hostname: hostname, Tags: tags, CreatedNotBefore: enrolledSince}
	if enrolledSince.IsZero() {
		q.NodeID = recordedNodeID
	}
	return q
}

// DeviceMatches applies one normalized hostname and tag identity policy.
func DeviceMatches(device Device, hostname string, tags []string) bool {
	want := strings.TrimSuffix(hostname, ".")
	if want == "" {
		return false
	}
	name := strings.TrimSuffix(device.Name, ".")
	deviceHost := strings.TrimSuffix(device.Hostname, ".")
	if deviceHost != want && name != want && !strings.HasPrefix(name, want+".") && !strings.HasPrefix(want, name+".") {
		return false
	}
	for _, tag := range tags {
		if !slices.Contains(device.Tags, tag) {
			return false
		}
	}
	return true
}

// SelectDevice returns the one device that satisfies q, or an error that says
// why no device can be trusted. Create, doctor, and import share it so every
// flow applies the same fail-closed rule before acting on a mesh node.
func SelectDevice(devices []Device, q DeviceQuery) (Device, error) {
	if q.NodeID != "" {
		return selectBoundDevice(devices, q)
	}
	var matches []Device
	for _, device := range devices {
		if !DeviceMatches(device, q.Hostname, q.Tags) || !createdNotBefore(device, q.CreatedNotBefore) {
			continue
		}
		matches = append(matches, device)
	}
	switch len(matches) {
	case 0:
		return Device{}, fmt.Errorf("%w: %q", ErrDeviceNotFound, q.Hostname)
	case 1:
		return matches[0], nil
	default:
		return Device{}, fmt.Errorf("%w: %q matches devices %s; remove the stale or unexpected devices from the tailnet, then retry", ErrDeviceAmbiguous, q.Hostname, strings.Join(deviceIDs(matches), ", "))
	}
}

// selectBoundDevice re-checks the device recorded in state. A missing or
// renamed device is terminal: searching by name again could hand a recorded
// server's traffic to a different node. Both ID forms are accepted because
// older state recorded the legacy ID.
func selectBoundDevice(devices []Device, q DeviceQuery) (Device, error) {
	for _, device := range devices {
		if device.ID != q.NodeID && device.NodeID != q.NodeID {
			continue
		}
		if !DeviceMatches(device, q.Hostname, q.Tags) {
			return Device{}, fmt.Errorf("%w: device %s has name %q hostname %q tags %v, want %q with tags %v", ErrBoundDeviceMismatch, q.NodeID, device.Name, device.Hostname, device.Tags, q.Hostname, q.Tags)
		}
		return device, nil
	}
	return Device{}, fmt.Errorf("%w: device %s", ErrBoundDeviceMissing, q.NodeID)
}

// createdNotBefore keeps a device only when its control-plane creation time
// is known and not earlier than the bound. An unparsable time fails closed.
func createdNotBefore(device Device, bound time.Time) bool {
	if bound.IsZero() {
		return true
	}
	created, err := time.Parse(time.RFC3339, device.Created)
	if err != nil {
		return false
	}
	return !created.Before(bound)
}

// deviceIDs lists stable identifiers for an operator to find and remove the
// conflicting devices in the admin console.
func deviceIDs(devices []Device) []string {
	ids := make([]string, 0, len(devices))
	for _, device := range devices {
		ids = append(ids, device.StableID())
	}
	slices.Sort(ids)
	return ids
}
