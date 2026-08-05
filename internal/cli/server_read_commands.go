package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/doctor"
	"github.com/assagman/serverpro/internal/passwordhash"
	"github.com/assagman/serverpro/internal/redact"
	"github.com/assagman/serverpro/internal/state"
	"gopkg.in/yaml.v3"
)

type serverReadRow struct {
	Namespace  string `json:"namespace"`
	Server     string `json:"server"`
	Provider   string `json:"provider"`
	Location   string `json:"location,omitempty"`
	Size       string `json:"size,omitempty"`
	Image      string `json:"image,omitempty"`
	Power      string `json:"power,omitempty"`
	PublicIPv4 string `json:"public_ipv4,omitempty"`
	Tailscale  string `json:"tailscale,omitempty"`
	SSH        string `json:"ssh,omitempty"`
	Ingress    string `json:"ingress,omitempty"`
}

type sshDryRunRow struct {
	DryRun  bool     `json:"dry_run"`
	Target  string   `json:"target"`
	Command []string `json:"command"`
}

type serverDoctorDryRunRow struct {
	Status    string `json:"status"`
	Action    string `json:"action"`
	DryRun    bool   `json:"dry_run"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
}

func (a *app) runServerList(ctx context.Context) error {
	rows, err := a.localServerRows(ctx)
	if err != nil {
		return err
	}
	return a.writeServerRows(rows)
}

func (a *app) runServerStatus(ctx context.Context, name string) error {
	if a.all {
		rows, err := a.serverRowsForName(ctx, name)
		if err != nil {
			return err
		}
		return a.writeServerRows(rows)
	}
	stPath, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	row, err := a.refreshServerRow(ctx, stPath, st)
	if err != nil {
		return err
	}
	return writeJSON(a.stdout, row)
}

func (a *app) runServerDoctor(ctx context.Context, name string) error {
	if a.doctorFix && a.dryRun {
		return errors.New("--fix cannot be used with --dry-run")
	}
	cfg, _, st, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	if a.dryRun {
		row := serverDoctorDryRunRow{Status: "planned", Action: "doctor", DryRun: true, Namespace: cfg.Project, Server: targetServer(cfg.Server)}
		return writeJSON(a.stdout, row)
	}
	creds, _, err := a.ensureCredentials(cfg)
	if err != nil {
		return err
	}
	var sudoPassword, adminPasswordHash string
	remoteTarget := st.Tailscale.Name != ""
	if remoteTarget {
		hasSudoEnv, err := sudoPasswordEnvSet(cfg)
		if err != nil {
			return err
		}
		if a.doctorFix || hasSudoEnv {
			label := "remote admin sudo password"
			if a.doctorFix {
				label = "remote admin sudo password to set/use"
			}
			sudoPassword, err = a.resolveSudoPasswordWithLabel(cfg, label)
			if err != nil {
				return err
			}
			if a.doctorFix {
				adminPasswordHash, err = passwordhash.GenerateSHA512(sudoPassword)
				if err != nil {
					return redact.New(a.redactionSecrets(creds)...).Error(err)
				}
				a.addRuntimeSecret(adminPasswordHash)
			}
		}
	}
	doctorCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	report := a.doctorReport(doctorCtx, cfg, st, creds, sudoPassword, adminPasswordHash)
	if remoteTarget && !a.doctorFix && sudoPassword == "" && reportNeedsSudoPassword(report) {
		sudoPassword, err = a.resolveSudoPassword(cfg)
		if err != nil {
			return err
		}
		report = a.doctorReport(doctorCtx, cfg, st, creds, sudoPassword, adminPasswordHash)
	}
	if err := report.Write(a.stdout, a.jsonOut); err != nil {
		return redact.New(a.redactionSecrets(creds)...).Error(err)
	}
	if !report.Passed() {
		return errors.New("doctor failed")
	}
	return nil
}

func reportNeedsSudoPassword(report doctor.Report) bool {
	return slices.ContainsFunc(report.Results, doctor.IsSudoPasswordAuthFailure)
}

func (a *app) runServerSSH(ctx context.Context, name string) error {
	ref, err := a.loadServerReadRef(name)
	if err != nil {
		return err
	}
	st := ref.State
	host := st.Tailscale.Name
	if host == "" {
		return fmt.Errorf("tailscale host missing from state; run serverpro server doctor %s", name)
	}
	user, err := a.resolveServerSSHUser(st, ref.ConfigPath)
	if err != nil {
		return err
	}
	target := user + "@" + host
	if a.dryRun {
		row := sshDryRunRow{DryRun: true, Target: target, Command: []string{"tailscale", "ssh", target}}
		return writeJSON(a.stdout, row)
	}
	execCmd := exec.CommandContext(ctx, "tailscale", "ssh", target)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	return execCmd.Run()
}

func (a *app) resolveServerSSHUser(st state.State, cfgPath string) (string, error) {
	if cfgPath == "" {
		cfgPath = config.ServerConfigPath(st.Project, st.Server)
	}
	if username := serverConfigAdminUsername(cfgPath); username != "" {
		return username, nil
	}
	// WHY: recovered/imported configs may omit admin user; never invent deploy for SSH.
	if a.nonInteractive {
		return "", fmt.Errorf("admin username missing in config; pass via config or re-import with --admin-user")
	}
	user, err := a.prompt("admin username")
	if err != nil {
		return "", err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("admin username required")
	}
	if err := a.persistAdminUsername(cfgPath, st, user); err != nil {
		return "", err
	}
	return user, nil
}

func (a *app) persistAdminUsername(cfgPath string, st state.State, user string) error {
	cfg, err := config.LoadPartial(cfgPath)
	if err != nil {
		// Config may be incomplete after partial recovery; write minimal admin field only when load fails hard.
		cfg = config.ExampleServer(st.Project, st.Server)
		cfg.Admin.Username = ""
	}
	cfg.Admin.Username = user
	if cfg.Project == "" {
		cfg.Project = st.Project
	}
	if cfg.Server == "" {
		cfg.Server = st.Server
	}
	return config.Save(cfgPath, cfg)
}

func serverConfigAdminUsername(path string) string {
	body, err := os.ReadFile(config.Expand(path))
	if err != nil {
		return ""
	}
	var data struct {
		Admin struct {
			Username string `yaml:"username"`
		} `yaml:"admin"`
	}
	if err := yaml.Unmarshal(body, &data); err != nil {
		return ""
	}
	return strings.TrimSpace(data.Admin.Username)
}

func (a *app) writeServerRows(rows []serverReadRow) error {
	return writeJSON(a.stdout, rows)
}

func (a *app) serverRowsForName(ctx context.Context, name string) ([]serverReadRow, error) {
	matches, err := a.serverStateMatches(name)
	if err != nil {
		return nil, err
	}
	rows := make([]serverReadRow, 0, len(matches))
	for _, match := range matches {
		row, err := a.refreshServerRow(ctx, match.StatePath, match.State)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (a *app) localServerRows(ctx context.Context) ([]serverReadRow, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return nil, err
	}
	var rows []serverReadRow
	for _, entry := range reg.List(a.project) {
		st, found, err := loadRegisteredState(entry.StatePath)
		if err != nil {
			return nil, err
		}
		if !found || !a.matchesServerState(st) {
			continue
		}
		rows = append(rows, serverRowFromState(st))
	}
	return rows, nil
}

func (a *app) refreshServerRow(ctx context.Context, stPath string, st state.State) (serverReadRow, error) {
	provider, accountRef, err := a.serverProviderAccount(st)
	if err != nil {
		return serverReadRow{}, err
	}
	status, diagnostics := provider.Status(ctx, compute.ServerRef{Account: accountRef, Record: serverRecordFromState(st)})
	if !diagnostics.Passed() {
		return serverReadRow{}, diagnostics.Err()
	}
	if status.Record.ID != "" && (st.Compute.PublicIPv4 != status.PublicIPv4 || st.Compute.PublicIPv6 != status.PublicIPv6) {
		st.Compute.PublicIPv4 = status.PublicIPv4
		st.Compute.PublicIPv6 = status.PublicIPv6
		_ = state.Save(config.Expand(stPath), st)
	}
	row := serverRowFromState(st)
	row.Power = statusPowerLabel(status.Power)
	if status.PublicIPv4 != "" {
		row.PublicIPv4 = status.PublicIPv4
	}
	return row, nil
}

func (a *app) loadServerReadState(name string) (string, state.State, error) {
	ref, err := a.loadServerReadRef(name)
	if err != nil {
		return "", state.State{}, err
	}
	return ref.StatePath, ref.State, nil
}

type serverReadRef struct {
	StatePath  string
	ConfigPath string
	State      state.State
}

func (a *app) loadServerReadRef(name string) (serverReadRef, error) {
	if a.project == "" {
		match, err := a.resolveServerStateMatch(name)
		if err != nil {
			return serverReadRef{}, err
		}
		a.project = match.State.Project
		if a.provider == "" {
			a.provider = match.State.Compute.Provider
		}
		return serverReadRef{StatePath: match.StatePath, ConfigPath: match.ConfigPath, State: match.State}, nil
	}
	stPath := a.statePath
	cfgPath := ""
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return serverReadRef{}, err
	}
	if entry, ok := reg.Find(a.project, name); ok {
		if stPath == "" {
			stPath = entry.StatePath
		}
		cfgPath = entry.ConfigPath
	}
	if stPath == "" {
		stPath = config.ServerStatePath(a.project, name)
	}
	st, err := state.Load(config.Expand(stPath))
	if err != nil {
		return serverReadRef{}, err
	}
	if st.Server != name {
		return serverReadRef{}, fmt.Errorf("state server %q does not match requested server %q", st.Server, name)
	}
	if !a.matchesServerState(st) {
		return serverReadRef{}, fmt.Errorf("state does not match requested provider")
	}
	return serverReadRef{StatePath: stPath, ConfigPath: cfgPath, State: st}, nil
}

type serverStateMatch struct {
	StatePath     string
	ConfigPath    string
	ResourceNames state.RegistryResourceNames
	State         state.State
}

func (a *app) resolveServerStateMatch(name string) (serverStateMatch, error) {
	matches, err := a.serverStateMatches(name)
	if err != nil {
		return serverStateMatch{}, err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return serverStateMatch{}, fmt.Errorf("no servers matched %q", name)
	}
	if a.nonInteractive {
		return serverStateMatch{}, a.ambiguousServerError(name, matches)
	}
	return a.promptServerMatch(name, matches)
}

func (a *app) serverStateMatches(name string) ([]serverStateMatch, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return nil, err
	}
	var matches []serverStateMatch
	for _, entry := range reg.List(a.project) {
		if entry.Server != name {
			continue
		}
		st, found, err := loadRegisteredState(entry.StatePath)
		if err != nil {
			return nil, err
		}
		if !found || !a.matchesServerState(st) {
			continue
		}
		matches = append(matches, serverStateMatch{StatePath: entry.StatePath, ConfigPath: entry.ConfigPath, ResourceNames: entry.ResourceNames, State: st})
	}
	return matches, nil
}

func loadRegisteredState(path string) (state.State, bool, error) {
	expanded := config.Expand(path)
	st, err := state.Load(expanded)
	if err == nil {
		return st, true, nil
	}
	// Missing files are stale registry entries; every other read or schema error
	// must stop discovery so corrupted state cannot silently disappear.
	if errors.Is(err, os.ErrNotExist) {
		return state.State{}, false, nil
	}
	return state.State{}, false, fmt.Errorf("load registered state %s: %w", expanded, err)
}

func (a *app) ambiguousServerError(name string, matches []serverStateMatch) error {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "multiple servers matched %q\n\npass one:\n", name)
	for _, match := range matches {
		st := match.State
		_, _ = fmt.Fprintf(&b, "  serverpro server status %s -n %s -p %s\n", name, st.Project, st.Compute.Provider)
	}
	_, _ = fmt.Fprintf(&b, "\nor use:\n  serverpro server status %s --all", name)
	return fmt.Errorf("%s", b.String())
}

func (a *app) promptServerMatch(name string, matches []serverStateMatch) (serverStateMatch, error) {
	options := serverMatchOptions(matches)
	if selected, ok, err := a.selectServerMatchWithFZF(options); err != nil {
		return serverStateMatch{}, err
	} else if ok {
		for i, option := range options {
			if option == selected {
				return matches[i], nil
			}
		}
	}
	for i, match := range matches {
		st := match.State
		if _, err := fmt.Fprintf(a.promptWriter(), "%d. %s provider=%s\n", i+1, st.Project, st.Compute.Provider); err != nil {
			return serverStateMatch{}, err
		}
	}
	choice, err := a.promptDefault("select server", "1")
	if err != nil {
		return serverStateMatch{}, err
	}
	if choice == "" {
		choice = "1"
	}
	for i := range matches {
		if choice == fmt.Sprintf("%d", i+1) {
			return matches[i], nil
		}
	}
	return serverStateMatch{}, fmt.Errorf("invalid selection %q for %s", choice, name)
}

func serverMatchOptions(matches []serverStateMatch) []string {
	options := make([]string, 0, len(matches))
	for _, match := range matches {
		st := match.State
		options = append(options, fmt.Sprintf("%s provider=%s", st.Project, st.Compute.Provider))
	}
	return options
}

func (a *app) selectServerMatchWithFZF(options []string) (string, bool, error) {
	if a.selectServerMatch != nil {
		return a.selectServerMatch(options)
	}
	path, err := exec.LookPath("fzf")
	if err != nil {
		return "", false, nil
	}
	cmd := exec.Command(path)
	cmd.Stdin = strings.NewReader(strings.Join(options, "\n"))
	cmd.Stderr = a.stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false, nil
	}
	selected := strings.TrimSpace(out.String())
	return selected, selected != "", nil
}

func (a *app) matchesServerState(st state.State) bool {
	if a.project != "" && st.Project != a.project {
		return false
	}
	if a.provider != "" && st.Compute.Provider != a.provider {
		return false
	}
	return true
}

func serverRowFromState(st state.State) serverReadRow {
	return serverReadRow{Namespace: st.Project, Server: st.Server, Provider: st.Compute.Provider, Location: st.Compute.Location, Size: st.Compute.Size, Image: st.Compute.Image, PublicIPv4: st.Compute.PublicIPv4, Tailscale: tailscaleSummary(st), SSH: sshSummary(st), Ingress: ingressSummary(st)}
}

func serverRecordFromState(st state.State) compute.ServerRecord {
	return compute.ServerRecord{Provider: compute.ProviderName(st.Compute.Provider), Namespace: st.Project, Server: st.Server, ID: st.Compute.ID, Name: st.Compute.Name, Location: st.Compute.Location, Size: st.Compute.Size, Image: st.Compute.Image, PublicIPv4: st.Compute.PublicIPv4, PublicIPv6: st.Compute.PublicIPv6, ProviderState: st.Compute.ProviderState}
}

func tailscaleSummary(st state.State) string {
	if st.Tailscale.Name != "" {
		return st.Tailscale.Name
	}
	return "missing"
}

func sshSummary(st state.State) string {
	if st.Tailscale.Name != "" {
		return "ready"
	}
	return "missing"
}

func ingressSummary(st state.State) string {
	if len(st.Ingress) > 0 {
		item := st.Ingress[0]
		if item.Hostname != "" {
			return item.Type + ":" + item.Hostname
		}
		return item.Type
	}
	if st.Cloudflare.TunnelID != "" || st.Cloudflare.Name != "" {
		return "cloudflare-tunnel"
	}
	return "none"
}
