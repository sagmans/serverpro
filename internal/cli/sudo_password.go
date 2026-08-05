package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
)

const minSudoPasswordLength = 16

func (a *app) resolveSudoPassword(cfg config.Config) (string, error) {
	return a.resolveSudoPasswordWithLabel(cfg, "remote admin sudo password")
}

func (a *app) resolveSudoPasswordWithLabel(cfg config.Config, label string) (string, error) {
	envName, err := sudoPasswordEnvName(cfg.Project, cfg.Server)
	if err != nil {
		return "", err
	}
	key := cfg.Project + "\x00" + cfg.Server
	if a.sudoPasswords != nil {
		if password, ok := a.sudoPasswords[key]; ok {
			return password, nil
		}
	}
	if password := os.Getenv(envName); password != "" {
		return a.cacheSudoPassword(key, password)
	}
	if a.nonInteractive {
		return "", fmt.Errorf("sudo password required in non-interactive mode; set %s", envName)
	}
	password, err := a.promptSecret(label)
	if err != nil {
		return "", err
	}
	return a.cacheSudoPassword(key, password)
}

func sudoPasswordEnvSet(cfg config.Config) (bool, error) {
	envName, err := sudoPasswordEnvName(cfg.Project, cfg.Server)
	if err != nil {
		return false, err
	}
	return os.Getenv(envName) != "", nil
}

func (a *app) cacheSudoPassword(key, password string) (string, error) {
	password = strings.TrimRight(password, "\r\n")
	if err := validateSudoPassword(password); err != nil {
		return "", err
	}
	if a.sudoPasswords == nil {
		a.sudoPasswords = map[string]string{}
	}
	a.sudoPasswords[key] = password
	a.addRuntimeSecret(password)
	return password, nil
}

func validateSudoPassword(password string) error {
	password = strings.TrimRight(password, "\r\n")
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("sudo password must not be empty or all whitespace")
	}
	if strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("sudo password must not contain line breaks")
	}
	if len(password) < minSudoPasswordLength {
		return fmt.Errorf("sudo password must be at least %d characters", minSudoPasswordLength)
	}
	return nil
}

func sudoPasswordEnvName(project, server string) (string, error) {
	if project == "" {
		return "", fmt.Errorf("namespace required for sudo password env var")
	}
	if server == "" {
		return "", fmt.Errorf("server required for sudo password env var")
	}
	return encodeEnvPart(project) + "_" + encodeEnvPart(server) + "_SUDOPASS", nil
}

func encodeEnvPart(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c - 'a' + 'A')
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			_, _ = fmt.Fprintf(&b, "_X%02X_", c)
		}
	}
	return b.String()
}

func (a *app) addRuntimeSecret(secret string) {
	if slices.Contains(a.runtimeSecrets, secret) {
		return
	}
	a.runtimeSecrets = append(a.runtimeSecrets, secret)
}

func (a *app) redactionSecrets(creds credentials.Set, extra ...string) []string {
	secrets := append([]string{}, creds.Secrets()...)
	secrets = append(secrets, a.runtimeSecrets...)
	secrets = append(secrets, extra...)
	return secrets
}
