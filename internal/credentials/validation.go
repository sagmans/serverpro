package credentials

import (
	"fmt"

	"github.com/assagman/serverpro/internal/config"
)

func (s Set) Validate() error {
	return s.validate("", "", false, s.Missing())
}

func (s Set) ValidateForProject(project string) error {
	return s.validate(project, "", true, s.Missing())
}

func (s Set) ValidateForConfig(cfg config.Config) error {
	return s.validate(cfg.Project, cfg.Server, true, s.MissingForConfig(cfg))
}

func (s Set) ValidateProject(project string) error {
	return s.validate(project, "", true, nil)
}

func (s Set) ValidateTarget(project, server string) error {
	return s.validate(project, server, true, nil)
}

func (s Set) validate(project, server string, requireProject bool, missing []string) error {
	s.normalizeNamespace()
	if requireProject {
		if project == "" {
			return fmt.Errorf("credentials namespace required")
		}
		if s.Project != project {
			return fmt.Errorf("credentials namespace %q does not match config namespace %q", s.Project, project)
		}
		if server != "" && s.Server != server {
			return fmt.Errorf("credentials server %q does not match config server %q", s.Server, server)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing credentials: %v", missing)
	}
	if s.TSAuthKey != "" {
		return fmt.Errorf("tailscale_auth_key cannot be verified as project-scoped; use tailscale_token")
	}
	return nil
}

func (s Set) Missing() []string {
	var missing []string
	if s.ServerProvider == "" {
		missing = append(missing, "server provider API token")
	}
	if s.Tailscale == "" {
		missing = append(missing, "Tailscale API token")
	}
	if s.Cloudflare == "" {
		missing = append(missing, "Cloudflare API token")
	}
	return missing
}

func (s Set) MissingForConfig(cfg config.Config) []string {
	var missing []string
	if s.ServerProvider == "" {
		missing = append(missing, "server provider API token")
	}
	if cfg.Access.Tailscale.Enabled && s.Tailscale == "" {
		missing = append(missing, "Tailscale API token")
	}
	if cfg.Cloudflare.Tunnel.Enabled && s.Cloudflare == "" {
		missing = append(missing, "Cloudflare API token")
	}
	return missing
}

func (s *Set) normalizeNamespace() {
	if s.Project == "" {
		s.Project = s.Namespace
	}
	if s.Namespace == "" {
		s.Namespace = s.Project
	}
}

func (s Set) Secrets() []string {
	return []string{s.ServerProvider, s.Tailscale, s.TSAuthKey, s.Cloudflare}
}
