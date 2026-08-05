package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c Config) validateNamespaceScopes() error {
	if !CredentialsPathScopedToServer(c.Credentials.JSONPath, c.Namespace, c.Server) {
		return fmt.Errorf("credentials.json_path must be scoped to server %q in namespace %q", c.Server, c.Namespace)
	}
	for _, tag := range c.Access.Tailscale.Tags {
		if !validTailscaleTag(tag) {
			return fmt.Errorf("invalid tailscale tag %q: use lowercase letters, numbers, and hyphens after tag", tag)
		}
	}
	if !tailscaleTagsScopedToNamespace(c.Access.Tailscale.Tags, c.Namespace) {
		return fmt.Errorf("access.tailscale.tags must include namespace %q", c.Namespace)
	}
	if !nameScopedToNamespace(c.Cloudflare.Tunnel.Name, c.Namespace) {
		return fmt.Errorf("cloudflare.tunnel.name must be scoped to namespace %q", c.Namespace)
	}
	return nil
}

func CredentialsPathScopedToServer(path, namespace, server string) bool {
	if namespace == "" || server == "" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	if len(parts) < 5 {
		return false
	}
	name := parts[len(parts)-1]
	serverDir := parts[len(parts)-2]
	serversDir := parts[len(parts)-3]
	namespaceDir := parts[len(parts)-4]
	namespacesDir := parts[len(parts)-5]
	return name == "credentials.json" && serverDir == server && serversDir == "servers" && namespaceDir == namespace && namespacesDir == "namespaces"
}

func tailscaleTagsScopedToNamespace(tags []string, namespace string) bool {
	if namespace == "" {
		return false
	}
	want := NamespaceTailscaleTag(namespace)
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func validTailscaleTag(tag string) bool {
	if !strings.HasPrefix(tag, "tag:") || len(tag) <= len("tag:") {
		return false
	}
	name := strings.TrimPrefix(tag, "tag:")
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func nameScopedToNamespace(name, namespace string) bool {
	prefix := slug(namespace)
	return name == prefix || strings.HasPrefix(name, prefix+"-")
}
