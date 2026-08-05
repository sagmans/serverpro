package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c Config) validateProjectScopes() error {
	if !CredentialsPathScopedToServer(c.Credentials.JSONPath, c.Project, c.Server) {
		return fmt.Errorf("credentials.json_path must be scoped to server %q in namespace %q", c.Server, c.Project)
	}
	for _, tag := range c.Access.Tailscale.Tags {
		if !validTailscaleTag(tag) {
			return fmt.Errorf("invalid tailscale tag %q: use lowercase letters, numbers, and hyphens after tag", tag)
		}
	}
	if !tailscaleTagsScopedToProject(c.Access.Tailscale.Tags, c.Project) {
		return fmt.Errorf("access.tailscale.tags must include namespace %q", c.Project)
	}
	if !nameScopedToProject(c.Cloudflare.Tunnel.Name, c.Project) {
		return fmt.Errorf("cloudflare.tunnel.name must be scoped to namespace %q", c.Project)
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

func CredentialsPathScopedToNamespace(path, namespace string) bool {
	return CredentialsPathScopedToServer(path, namespace, "server")
}

func CredentialsPathScopedToProject(path, project string) bool {
	return CredentialsPathScopedToNamespace(path, project)
}

func tailscaleTagsScopedToProject(tags []string, project string) bool {
	if project == "" {
		return false
	}
	want := ProjectTailscaleTag(project)
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

func nameScopedToProject(name, project string) bool {
	prefix := slug(project)
	return name == prefix || strings.HasPrefix(name, prefix+"-")
}
