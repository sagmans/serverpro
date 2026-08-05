package doctor

import "strings"

func summarizeRemoteEvidence(name, out string) string {
	text := strings.TrimSpace(out)
	if text == "" {
		return "ok"
	}
	if text == "ok" {
		return text
	}
	switch name {
	case "cloud-init":
		return firstMatchingLine(text, "status:")
	case "ufw active":
		return "active"
	case "ufw default deny incoming":
		return "deny incoming"
	case "ufw ssh ingress":
		if strings.Contains(strings.ToLower(text), "no ssh allow in rules") {
			return "no SSH ALLOW IN rules"
		}
	case "apparmor":
		return "active/enabled"
	case "unattended upgrades":
		return "enabled"
	case "journald persistent":
		return "persistent"
	case "listening ports":
		return summarizeListeningPorts(text)
	case "egress positive":
		return "dns/http egress ok"
	case "cloudflared":
		return "active"
	}
	return trim(text)
}

func firstMatchingLine(text, sub string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(sub)) {
			return trim(strings.TrimSpace(line))
		}
	}
	return trim(text)
}
