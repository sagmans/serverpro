package config

import (
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/servername"
)

func ServerResourceName(namespace, server string) string {
	prefix := slug(namespace)
	if servername.Normalize(server) == servername.Default {
		return prefix + "-01"
	}
	return prefix + "-" + slug(server)
}

func NamespaceTailscaleTag(namespace string) string {
	if namespace == "" {
		return "tag:serverpro-server"
	}
	return "tag:serverpro-" + slug(namespace)
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		_, _ = fmt.Fprintf(&b, "-x%x-", r)
	}
	if b.Len() == 0 {
		return "server"
	}
	return b.String()
}
