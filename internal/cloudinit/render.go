// Package cloudinit renders cloud-init user data for serverpro-managed hosts.
package cloudinit

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/sagmans/serverpro/internal/config"
)

// Input carries no admin password material: user data is readable from the
// provider metadata service, so the password is set over SSH after first boot.
type Input struct {
	Config           config.Config
	TailscaleAuthKey string
}

func Render(in Input) (string, error) {
	if in.Config.Admin.Username == "" {
		return "", fmt.Errorf("admin username required")
	}
	if in.TailscaleAuthKey == "" {
		return "", fmt.Errorf("tailscale auth key required")
	}
	var b bytes.Buffer
	t, err := template.New("cloudinit").Funcs(templateFuncs()).Parse(cloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse cloud-init template: %w", err)
	}
	if err := t.Execute(&b, in); err != nil {
		return "", err
	}
	return b.String(), nil
}
