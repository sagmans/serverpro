package mesh

import (
	"slices"
	"strings"
)

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
