// Package cloudinit renders cloud-init user data for serverpro-managed hosts.
package cloudinit

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/passwordhash"
)

type Input struct {
	Config            config.Config
	TailscaleAuthKey  string
	AdminPasswordHash string
}

func Render(in Input) (string, error) {
	if in.Config.Admin.Username == "" {
		return "", fmt.Errorf("admin username required")
	}
	if in.TailscaleAuthKey == "" {
		return "", fmt.Errorf("tailscale auth key required")
	}
	if !passwordhash.ValidSHA512(in.AdminPasswordHash) {
		return "", fmt.Errorf("admin password hash required")
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
