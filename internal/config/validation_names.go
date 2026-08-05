package config

import (
	"fmt"
	"strings"
)

func (c Config) validateResourceNames() error {
	if !ValidID(c.Project) {
		return fmt.Errorf("invalid namespace %q", c.Project)
	}
	if !ValidID(c.Server) {
		return fmt.Errorf("invalid server %q", c.Server)
	}
	if !validProviderResourceName(c.Compute.Name) {
		return fmt.Errorf("invalid compute.name %q: use 1-63 letters, numbers, and hyphens; start/end with a letter or number", c.Compute.Name)
	}
	if !validProviderResourceName(c.Cloudflare.Tunnel.Name) {
		return fmt.Errorf("invalid cloudflare.tunnel.name %q: use 1-63 letters, numbers, and hyphens; start/end with a letter or number", c.Cloudflare.Tunnel.Name)
	}
	return nil
}

func (c Config) validateAdminUsername() error {
	if !validUsername(c.Admin.Username) {
		return fmt.Errorf("invalid admin username %q", c.Admin.Username)
	}
	return nil
}

func validProviderResourceName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		if !isProviderResourceChar(r) {
			return false
		}
		if (i == 0 || i == len(s)-1) && !isASCIIAlnum(r) {
			return false
		}
	}
	return true
}

func isProviderResourceChar(r rune) bool {
	return isASCIIAlnum(r) || r == '-'
}

func isASCIIAlnum(r rune) bool {
	return isASCIILower(r) || isASCIIUpper(r) || isASCIIDigit(r)
}

func isASCIILower(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isASCIIUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isASCIILowerOrDigit(r rune) bool {
	return isASCIILower(r) || isASCIIDigit(r)
}

func isIDChar(r rune) bool {
	return isASCIILowerOrDigit(r) || r == '-' || r == '_' || r == '.'
}

func isUsernameChar(r rune) bool {
	return isASCIILowerOrDigit(r) || r == '-' || r == '_'
}

func ValidID(s string) bool {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\") {
		return false
	}
	for i, r := range s {
		if !isIDChar(r) {
			return false
		}
		if (i == 0 || i == len(s)-1) && !isASCIILowerOrDigit(r) {
			return false
		}
	}
	return true
}

func validUsername(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for i, r := range s {
		if !isUsernameChar(r) || i == 0 && !isASCIILower(r) {
			return false
		}
	}
	return true
}
