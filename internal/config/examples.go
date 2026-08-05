package config

import (
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/servername"
)

func Example(project string) Config {
	return ExampleServer(project, servername.Default)
}

func ExampleServer(project, server string) Config {
	cfg := Default()
	cfg.Project = project
	cfg.Namespace = project
	server = servername.Normalize(server)
	cfg.Server = server
	cfg.Credentials.JSONPath = ServerCredentialsPath(project, server)
	cfg.Access.Tailscale.Tags = []string{ProjectTailscaleTag(project)}
	cfg.Compute.Name = ServerResourceName(project, server)
	cfg.Cloudflare.Tunnel.Name = cfg.Compute.Name
	cfg.Compute.Labels = ownership.ConfigLabels(project, server, cfg.Compute.Labels)
	return cfg
}
