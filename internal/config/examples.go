package config

import (
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/servername"
)

func Example(namespace string) Config {
	return ExampleServer(namespace, servername.Default)
}

func ExampleServer(namespace, server string) Config {
	cfg := Default()
	cfg.Namespace = namespace
	server = servername.Normalize(server)
	cfg.Server = server
	cfg.Credentials.JSONPath = ServerCredentialsPath(namespace, server)
	cfg.Access.Tailscale.Tags = []string{NamespaceTailscaleTag(namespace)}
	cfg.Compute.Name = ServerResourceName(namespace, server)
	cfg.Cloudflare.Tunnel.Name = cfg.Compute.Name
	cfg.Compute.Labels = ownership.ConfigLabels(namespace, server, cfg.Compute.Labels)
	return cfg
}
