// Package bootstraptools_test holds guards that span every Go-rendered script
// run as root on managed hosts. It is an external test package so it can
// import cloudinit, tunnel and tailscaletools beside bootstraptools without an
// import cycle.
package bootstraptools_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/cloudinit"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/tailscaletools"
	"github.com/sagmans/serverpro/internal/tunnel"
)

const (
	// guardTestUser, guardTestNamespace, guardTestAccountID,
	// guardTestTailscaleKey and guardTestTunnelToken only fill the render
	// inputs; the guard reads the scripts, never runs them.
	guardTestUser         = "deploy"
	guardTestNamespace    = "prod"
	guardTestAccountID    = "acc"
	guardTestTailscaleKey = "tskey-auth-oneoff"
	guardTestTunnelToken  = "cf-tunnel-token"
	// shellLineContinuation is the trailing backslash that joins a shell
	// command across physical lines.
	shellLineContinuation = `\`
	// shellCommentPrefix starts a comment line, which cannot run sudo.
	shellCommentPrefix = "#"
)

// sudoUserSwitch matches a sudo call that switches user or sets HOME, the form
// that re-enters PAM's account stage for root.
var sudoUserSwitch = regexp.MustCompile(`(^|[^[:alnum:]_-])sudo(\s+-[^\s]+)*\s+(-[A-Za-z]*[uHi][A-Za-z]*|--user(=|\s)|--set-home|--login)`)

// logicalLine is one shell command as the shell reads it, with the physical
// line it starts on so a finding points at the source.
type logicalLine struct {
	start int
	text  string
}

// logicalLines joins backslash continuations so a call split as "sudo \" then
// "-u user" is matched as one command. A comment line never continues, as in
// the shell, so it cannot swallow the command after it.
func logicalLines(script string) []logicalLine {
	var lines []logicalLine
	var pending strings.Builder
	start := 0
	for i, physical := range strings.Split(script, "\n") {
		if pending.Len() == 0 {
			if strings.HasPrefix(strings.TrimSpace(physical), shellCommentPrefix) {
				continue
			}
			start = i + 1
		}
		if body, ok := strings.CutSuffix(physical, shellLineContinuation); ok {
			pending.WriteString(body)
			pending.WriteByte(' ')
			continue
		}
		pending.WriteString(physical)
		lines = append(lines, logicalLine{start: start, text: pending.String()})
		pending.Reset()
	}
	if pending.Len() > 0 {
		lines = append(lines, logicalLine{start: start, text: pending.String()})
	}
	return lines
}

func sudoUserSwitches(script string) []logicalLine {
	var found []logicalLine
	for _, line := range logicalLines(script) {
		if sudoUserSwitch.MatchString(line.text) {
			found = append(found, line)
		}
	}
	return found
}

func TestSudoUserSwitchGuardMatches(t *testing.T) {
	for _, script := range []string{`sudo -u deploy true`, `sudo -H -u "$TARGET_USER" bash`, `x && sudo -Hu deploy id`, `sudo --user=deploy id`, `sudo -n -i`, "sudo \\\n  -u deploy id", "sudo -n \\\n\t--login"} {
		if len(sudoUserSwitches(script)) == 0 {
			t.Errorf("guard misses %q", script)
		}
	}
	for _, script := range []string{`runuser -u deploy -- id`, `sudo -k`, `sudo -n true`, `pseudo -u x`, "# sudo -u deploy id", "# note \\\nsudo -n true", "sudo \\\n  -n true"} {
		if found := sudoUserSwitches(script); len(found) != 0 {
			t.Errorf("guard flags %q: %+v", script, found)
		}
	}
}

// hostScripts renders every Go-embedded script that runs as root on a managed
// host, so one guard covers them all instead of a copy per package.
func hostScripts(t *testing.T) map[string]string {
	t.Helper()
	cfg := config.Example(guardTestNamespace)
	cfg.Cloudflare.AccountID = guardTestAccountID
	userData, err := cloudinit.Render(cloudinit.Input{Config: cfg, TailscaleAuthKey: guardTestTailscaleKey})
	if err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"bootstraptools install script":  bootstraptools.InstallScriptForUser(guardTestUser),
		"bootstraptools manual wrapper":  bootstraptools.WrapperScript(),
		"bootstraptools package refresh": bootstraptools.ManagedPackageRefreshCommand(),
		"cloudinit user data":            userData,
		"tunnel install script":          tunnel.InstallScript(guardTestTunnelToken),
		"tunnel upgrade script":          tunnel.UpgradeScript(),
		"tunnel check command":           tunnel.CheckCommand(),
		"tailscaletools update script":   tailscaletools.UpdateScript(),
		"tailscaletools check command":   tailscaletools.CheckCommand(),
	}
	for _, check := range bootstraptools.Checks(guardTestUser) {
		scripts["bootstraptools check "+check.Name] = check.Command
	}
	return scripts
}

// TestHostScriptsNeverSwitchUserThroughSudo keeps sudo -u / -H / -i out of
// every script that runs as root on a host: such a drop fails on hosts whose
// root password is expired (see run_as_target), so they must use runuser.
func TestHostScriptsNeverSwitchUserThroughSudo(t *testing.T) {
	for name, script := range hostScripts(t) {
		for _, line := range sudoUserSwitches(script) {
			t.Errorf("%s line %d drops privileges through sudo; use runuser (run_as_target): %s", name, line.start, strings.TrimSpace(line.text))
		}
	}
}
