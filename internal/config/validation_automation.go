package config

import (
	"fmt"
	"path/filepath"
)

// validateTailscaleAutomation keeps the automation accept rule narrow. The
// paths go into an ssh ProxyCommand that a shell parses, so only plain path
// characters are allowed (deny by default), and the automation tag can never
// also be a server tag.
func (c Config) validateTailscaleAutomation() error {
	a := c.Access.Tailscale.Automation
	for _, tag := range c.Access.Tailscale.Tags {
		if tag == AutomationTailscaleTag {
			return fmt.Errorf("access.tailscale.tags must not include %s: that tag is the automation SSH source", AutomationTailscaleTag)
		}
	}
	// With automation off the paths may stay, so a revoke is a one-key edit;
	// they are still checked, so turning it on later cannot pass a bad path.
	for _, field := range []struct{ name, path string }{{"socket", a.Socket}, {"cli", a.CLI}} {
		if !a.Enabled && field.path == "" {
			continue
		}
		if !safeProxyCommandPath(field.path) {
			return fmt.Errorf("access.tailscale.automation.%s must be an absolute path of letters, numbers, '/', '.', '_' or '-'", field.name)
		}
	}
	return nil
}

// safeProxyCommandPath allows only characters that no shell treats as special,
// so the path cannot change the ProxyCommand that ssh hands to /bin/sh.
func safeProxyCommandPath(path string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, r := range path {
		if isASCIIAlnum(r) || r == '/' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
