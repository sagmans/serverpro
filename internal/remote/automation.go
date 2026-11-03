package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/mesh"
)

const (
	// automationStatusTimeout bounds the local LocalAPI read. The tagged node
	// runs on this machine, so a slow answer means it is stuck, not far away.
	automationStatusTimeout = 15 * time.Second
	// automationBackendRunning is the tailscaled state in which the node has
	// a netmap and can dial peers.
	automationBackendRunning = "Running"
	// automationSSHClient is the system OpenSSH client, by absolute path so a
	// PATH entry cannot take the session and the sudo password on its stdin.
	// On macOS "tailscale ssh" never adds a ProxyCommand, so it cannot use a
	// second tailscaled; serverpro builds the same OpenSSH call itself.
	automationSSHClient = "/usr/bin/ssh"
	// automationStderrLimit keeps a failed status call's error readable.
	automationStderrLimit = 512
	// automationSSHLabel names this transport in errors.
	automationSSHLabel = "automation ssh"
	// knownHostsFileName is the per-run known_hosts file inside a private
	// temp directory, so a stale or shared file can never vouch for a host.
	knownHostsFileName   = "known_hosts"
	knownHostsDirPattern = "serverpro-automation-ssh-"
)

// ErrAutomationNodeNotReady means the tagged node cannot carry the session.
// Callers fail closed on it: a silent fall back to the operator's own device
// would hit the check rule and hang on a browser login (the failure this
// transport exists to remove).
var ErrAutomationNodeNotReady = errors.New("tailscale automation node not ready")

// ErrAutomationPeerNotReady means the node is usable but the target server is
// not in its netmap yet, or has no SSH host keys yet. Right after a create
// this clears by itself, so callers that wait for a new server retry it.
var ErrAutomationPeerNotReady = errors.New("tailscale automation peer not ready")

// AutomationNode is the tagged tailscaled on the operator machine that the
// automation SSH accept rule trusts.
type AutomationNode struct {
	// CLI is the tailscale binary for Socket. It must be an absolute path
	// because it is also the ProxyCommand that ssh runs.
	CLI string
	// Socket is the LocalAPI socket of the tagged tailscaled.
	Socket string
	// Tags are the server's tags. The target peer must carry one of them, so
	// a shared-in node or another device with the same name never gets the
	// session.
	Tags []string
	// SSH overrides the OpenSSH client path for tests; empty means the
	// system client.
	SSH string
}

// automationStatus is the part of "tailscale status --json" that this
// transport needs: the node must be up and carry the automation tag, and the
// target peer gives its name, addresses and SSH host keys.
type automationStatus struct {
	BackendState string                    `json:"BackendState"`
	Self         automationPeer            `json:"Self"`
	Peer         map[string]automationPeer `json:"Peer"`
}

type automationPeer struct {
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Tags         []string `json:"Tags"`
	// tailscale ipnstate.PeerStatus.SSH_HostKeys serializes as "sshHostKeys".
	SSHHostKeys []string `json:"sshHostKeys"`
}

func (n AutomationNode) run(ctx context.Context, timeout time.Duration, user, host, remoteCommand, stdin string, outputLimit int) (string, error) {
	status, err := n.status(ctx)
	if err != nil {
		return "", err
	}
	peer, err := status.readyPeer(host, n.Tags)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", knownHostsDirPattern)
	if err != nil {
		return "", fmt.Errorf("automation ssh known_hosts directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	knownHosts := filepath.Join(dir, knownHostsFileName)
	if err := os.WriteFile(knownHosts, peer.knownHosts(), 0o600); err != nil {
		return "", fmt.Errorf("automation ssh known_hosts write: %w", err)
	}
	args := n.sshArgs(knownHosts, user, strings.TrimSuffix(peer.DNSName, "."), remoteCommand)
	client := n.SSH
	if client == "" {
		client = automationSSHClient
	}
	return runSSHCommandWithOutputLimit(ctx, timeout, automationSSHLabel, client, args, stdin, outputLimit)
}

// sshArgs pins every OpenSSH option that matters, and -F /dev/null drops the
// user's ssh config, so a ControlMaster, ProxyJump or host alias there cannot
// send the session around the tagged node. BatchMode makes any prompt fail.
// Tailscale SSH authenticates the node, not a key, so no key or agent is
// offered: the peer learns nothing about the operator's other identities.
func (n AutomationNode) sshArgs(knownHosts, user, host, remoteCommand string) []string {
	return []string{
		"-F", "/dev/null",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "UpdateHostKeys=no",
		"-o", "CanonicalizeHostname=no",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "PubkeyAuthentication=no",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ProxyCommand=" + n.CLI + " --socket=" + n.Socket + " nc %h %p",
		"--",
		user + "@" + host,
		remoteCommand,
	}
}

func (n AutomationNode) status(ctx context.Context) (automationStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, automationStatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.CLI, "--socket="+n.Socket, "status", "--json")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if len(stderr) > automationStderrLimit {
				stderr = stderr[:automationStderrLimit]
			}
			return automationStatus{}, fmt.Errorf("%w: status via socket %s: %w: %s", ErrAutomationNodeNotReady, n.Socket, err, stderr)
		}
		return automationStatus{}, fmt.Errorf("%w: status via socket %s: %w", ErrAutomationNodeNotReady, n.Socket, err)
	}
	var status automationStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return automationStatus{}, fmt.Errorf("%w: status JSON via socket %s: %w", ErrAutomationNodeNotReady, n.Socket, err)
	}
	return status, nil
}

// readyPeer refuses a node that is down or untagged. An untagged node is the
// operator's own identity, which the accept rule does not cover. The target
// must be exactly one peer that carries a server tag: map order is random, so
// two candidates would make the choice random too.
func (s automationStatus) readyPeer(host string, tags []string) (automationPeer, error) {
	if s.BackendState != automationBackendRunning {
		return automationPeer{}, fmt.Errorf("%w: backend state %q, want %q", ErrAutomationNodeNotReady, s.BackendState, automationBackendRunning)
	}
	if !slices.Contains(s.Self.Tags, mesh.AutomationSourceTag) {
		return automationPeer{}, fmt.Errorf("%w: node tags %v do not include %s", ErrAutomationNodeNotReady, s.Self.Tags, mesh.AutomationSourceTag)
	}
	if len(tags) == 0 {
		return automationPeer{}, fmt.Errorf("%w: no server tags to check peer %s against", ErrAutomationNodeNotReady, host)
	}
	var found []automationPeer
	for _, peer := range s.Peer {
		if peer.matches(host) && slices.ContainsFunc(peer.Tags, func(tag string) bool { return slices.Contains(tags, tag) }) {
			found = append(found, peer)
		}
	}
	switch {
	case len(found) > 1:
		return automationPeer{}, fmt.Errorf("%w: %d peers with tags %v match %s; use the server FQDN", ErrAutomationNodeNotReady, len(found), tags, host)
	case len(found) == 0:
		return automationPeer{}, fmt.Errorf("%w: no peer %s with tags %v in the automation node netmap (check the packet grant to the server tag)", ErrAutomationPeerNotReady, host, tags)
	case len(found[0].SSHHostKeys) == 0:
		return automationPeer{}, fmt.Errorf("%w: peer %s has no Tailscale SSH host keys", ErrAutomationPeerNotReady, host)
	}
	return found[0], nil
}

// matches accepts the forms that state stores for a server: the MagicDNS
// FQDN, its first label, or a Tailscale IP.
func (p automationPeer) matches(host string) bool {
	if host == "" {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return slices.Contains(p.TailscaleIPs, ip.String())
	}
	name := strings.TrimSuffix(p.DNSName, ".")
	host = strings.TrimSuffix(host, ".")
	if strings.EqualFold(name, host) {
		return true
	}
	base, _, ok := strings.Cut(name, ".")
	return ok && strings.EqualFold(base, host)
}

// knownHosts trusts only this peer's keys as the control plane reports them,
// so StrictHostKeyChecking proves the session reached the intended server.
func (p automationPeer) knownHosts() []byte {
	var b strings.Builder
	name := strings.TrimSuffix(p.DNSName, ".")
	for _, key := range p.SSHHostKeys {
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, "\r\n") {
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", name, key)
	}
	return []byte(b.String())
}
