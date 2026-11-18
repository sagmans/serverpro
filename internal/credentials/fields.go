package credentials

import (
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
)

// providerTokenField is the provider API token. Its companion
// server_provider_name only describes which provider the token belongs to.
const providerTokenField = "server_provider_token"

// fieldSpec binds one stored field name (its JSON key, which operators see
// in docs and `auth list`) to the Set member it controls.
type fieldSpec struct {
	name  string
	value func(*Set) *string
}

// fieldSpecs is the allowlist `auth set` and `auth remove` accept. A new
// stored secret must be added here to be manageable; anything else is
// refused, so a typo can never write an unknown key into the file.
var fieldSpecs = []fieldSpec{
	{name: providerTokenField, value: func(s *Set) *string { return &s.ServerProvider }},
	{name: "server_provider_name", value: func(s *Set) *string { return &s.ServerProviderName }},
	{name: "tailscale_token", value: func(s *Set) *string { return &s.Tailscale }},
	{name: "tailscale_auth_key", value: func(s *Set) *string { return &s.TSAuthKey }},
	{name: "cloudflare_token", value: func(s *Set) *string { return &s.Cloudflare }},
	{name: "admin_sudo_password", value: func(s *Set) *string { return &s.AdminSudoPassword }},
	{name: "github_pat", value: func(s *Set) *string { return &s.GitHubPAT }},
}

// FieldNames lists the names `auth set` accepts, for help and errors.
func FieldNames() []string {
	names := make([]string, 0, len(fieldSpecs))
	for _, spec := range fieldSpecs {
		names = append(names, spec.name)
	}
	return names
}

// ValidateField rejects an unknown field name before the CLI reads a value,
// so an operator is not asked to paste a secret for a typo.
func ValidateField(name string) error {
	_, err := lookupField(name)
	return err
}

// SetField stores one value under the writer lock, keeping every other field
// exactly as a concurrent run last left it.
func SetField(cfg config.Config, name, value string) error {
	spec, err := lookupField(name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s: empty value; use auth remove to delete it", name)
	}
	return Update(cfg, func(set *Set) error {
		*spec.value(set) = value
		return nil
	})
}

// RemoveField clears one value. Removing the provider token also clears the
// provider it was recorded for, since that record only describes the token.
func RemoveField(cfg config.Config, name string) error {
	spec, err := lookupField(name)
	if err != nil {
		return err
	}
	return Update(cfg, func(set *Set) error {
		*spec.value(set) = ""
		if spec.name == providerTokenField {
			set.ServerProviderName = ""
		}
		return nil
	})
}

// ServerConfig builds the minimal config the credential seam needs for one
// namespace/server, so `auth` works before or without a server config file.
func ServerConfig(namespace, server string) config.Config {
	return config.Config{Namespace: namespace, Server: server, Credentials: config.Credentials{JSONPath: config.ServerCredentialsPath(namespace, server)}}
}

func lookupField(name string) (fieldSpec, error) {
	for _, spec := range fieldSpecs {
		if spec.name == name {
			return spec, nil
		}
	}
	return fieldSpec{}, fmt.Errorf("unknown credential field %q: use one of %s", name, strings.Join(FieldNames(), ", "))
}

// storedFieldNames names the fields that hold a value, never the values.
func (s Set) storedFieldNames() []string {
	var names []string
	for _, spec := range fieldSpecs {
		if *spec.value(&s) != "" {
			names = append(names, spec.name)
		}
	}
	return names
}
