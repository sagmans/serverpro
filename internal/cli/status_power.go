package cli

func statusPowerLabel(status string) string {
	if status == "running" {
		return "on"
	}
	return status
}
