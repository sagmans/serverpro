package config

import (
	"fmt"
	"strings"

	"github.com/assagman/serverpro/internal/servername"
)

func ServerResourceName(project, server string) string {
	prefix := slug(project)
	if servername.Normalize(server) == servername.Default {
		return prefix + "-01"
	}
	return prefix + "-" + slug(server)
}

func ProjectTailscaleTag(project string) string {
	if project == "" {
		return "tag:serverpro-server"
	}
	return "tag:serverpro-" + slug(project)
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
