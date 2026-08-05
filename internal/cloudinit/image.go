package cloudinit

import (
	"strings"
	"unicode"

	"github.com/assagman/serverpro/internal/compute"
)

const (
	supportedOSFlavor  = "ubuntu"
	supportedOSVersion = "24.04"
)

// SupportsImage keeps provider selection aligned with package sources and
// hardening commands rendered specifically for Ubuntu 24.04 LTS.
func SupportsImage(image compute.Image) bool {
	if !strings.EqualFold(strings.TrimSpace(image.OSFlavor), supportedOSFlavor) {
		return false
	}
	if strings.TrimSpace(image.OSVersion) == supportedOSVersion {
		return true
	}
	for _, token := range strings.FieldsFunc(image.Description, func(r rune) bool {
		return !unicode.IsDigit(r) && r != '.'
	}) {
		if token == supportedOSVersion {
			return true
		}
	}
	return false
}
