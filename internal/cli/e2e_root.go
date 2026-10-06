//go:build serverpro_e2e

package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

const (
	e2eAuthKeyID                     = "e2e-auth-key"
	e2eAuthKey                       = "tskey-auth-e2e"
	e2eDeviceID                      = "e2e-device"
	e2eDeviceIP                      = "100.64.0.10"
	e2eCheckpointError               = "injected completion checkpoint failure"
	e2eCheckpointFileMode            = 0o600
	e2eAuditEnv                      = "SERVERPRO_E2E_CLEANUP_AUDIT"
	e2ePreflightPolicyEvent          = "preflight-policy"
	e2eProvisionOptionsEvent         = "provision-options"
	e2eDeleteDeviceEvent             = "delete-device"
	e2eDeleteAuthKeyEvent            = "delete-auth-key"
	e2eRemoteSuccessEvidence         = "ok"
	e2eDeleteCleanupFailureEnv       = "SERVERPRO_E2E_DELETE_CLEANUP_FAILURE"
	e2eDeleteCleanupFailPreflight    = "preflight"
	e2eDeleteCleanupFailAfter        = "after-compute"
	e2eDeleteCleanupDevicesPath      = "/tailnet/-/devices"
	e2eDeleteCleanupUnauthorized     = "401 Unauthorized"
	e2eDeleteCleanupUnauthorizedBody = `{"message":"API token invalid"}`
	e2eDoctorPackageFailureEnv       = "SERVERPRO_E2E_DOCTOR_PACKAGE_FAILURE"
	e2eDoctorPackageFailureShort     = "short"
	e2eDoctorPackageFailureBoundary  = "boundary"
	e2eDoctorPackageEvidence         = "managed package below baseline: synthetic-package 0.1 < 1.0"
	e2eDoctorRepairCause             = "E: synthetic fixture package transaction rejected"
	e2eDoctorRepairErrorPrefix       = "tailscale ssh failed: exit status 100: "
	e2eDoctorNoise                   = "synthetic package metadata progress\n"
	e2eDoctorNoiseLines              = 256
	e2eDoctorEvidenceHeadBytes       = 1024
	e2eDoctorPadding                 = "."
	e2eDoctorPackageFailureStatus    = 1
)

var e2eNow = time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)

// NewE2E wires only loopback provider clients and deterministic local fakes.
func NewE2E(apiURL string) (*cobra.Command, error) {
	if err := validateE2EAPIURL(apiURL); err != nil {
		return nil, err
	}
	registry := e2eProviderRegistry(apiURL)
	a := &app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, providers: registry}
	tailscaleClient := e2eTailscale{}
	cleanupTailscale := &e2eCleanupTailscale{}
	a.services = serviceHooks{
		preflightTailscaleClient: func(string, string) preflightTailscaleClient {
			return tailscaleClient
		},
		provisionClients: func(clients lifecycle.Clients) lifecycle.Clients {
			clients.Tailscale = tailscaleClient
			return clients
		},
		provisionOptions: func(options lifecycle.Options) (lifecycle.Options, error) {
			if err := appendE2EAudit(e2eProvisionOptionsEvent, "configured"); err != nil {
				return options, err
			}
			return lifecycle.ConfigureE2E(options, func() time.Time { return e2eNow }, e2eSaveState), nil
		},
		doctorClients: func(_ context.Context, cfg config.Config, st state.State, creds credentials.Set, sudoPassword string) (doctor.Clients, compute.Account, error) {
			provider, ok := registry.Get(compute.ProviderName(st.Compute.Provider))
			if !ok {
				return doctor.Clients{}, compute.Account{}, errors.New("e2e provider missing")
			}
			account := compute.Account{Name: cfg.Namespace + "/" + cfg.Server, Provider: provider.Name(), Token: creds.ServerProvider, Scope: cfg.Namespace + "/" + cfg.Server}
			clients := doctor.Clients{
				Compute:   provider,
				Tailscale: tailscaleClient,
				Remote:    e2eDoctorRemote{secrets: []string{creds.ServerProvider, creds.Tailscale, sudoPassword}},
				PublicSSHProbe: func(context.Context, string) error {
					return syscall.ECONNREFUSED
				},
			}
			return clients, account, nil
		},
		cleanupClients: func(cleanup serverDeleteCleanup) serverCleanupClients {
			cleanupTailscale.state = cleanup.State
			return serverCleanupClients{Tailscale: cleanupTailscale}
		},
	}
	return newRoot(a), nil
}

func validateE2EAPIURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("e2e provider API URL must be an absolute HTTP URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("e2e provider API URL host must be loopback")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("e2e provider API URL must not contain credentials, query, fragment, or path")
	}
	return nil
}

func e2eSaveState(path string, st state.State) error {
	marker := os.Getenv("SERVERPRO_E2E_FAIL_COMPLETE_CHECKPOINT")
	if marker != "" && len(st.Validations) > 0 {
		if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
			if writeErr := os.WriteFile(marker, []byte("failed\n"), e2eCheckpointFileMode); writeErr != nil {
				return writeErr
			}
			return errors.New(e2eCheckpointError)
		} else if err != nil {
			return err
		}
	}
	return state.Save(path, st)
}

type e2eTailscale struct{}

func (e2eTailscale) Policy(context.Context) (mesh.Policy, error) {
	return mesh.Policy{}, appendE2EAudit(e2ePreflightPolicyEvent, "checked")
}

func (e2eTailscale) CreateAuthKey(context.Context, []string, time.Duration) (mesh.AuthKey, error) {
	return mesh.AuthKey{ID: e2eAuthKeyID, Key: e2eAuthKey}, nil
}

func (e2eTailscale) DeleteAuthKey(context.Context, string) error { return nil }

func (e2eTailscale) EnsureServerproPolicy(context.Context, []string, string, string) (mesh.PolicyChange, error) {
	return mesh.PolicyChange{}, nil
}

func (e2eTailscale) ValidateSSHPolicy(context.Context, []string, string, string) error { return nil }

func (e2eTailscale) WaitDevice(_ context.Context, q mesh.DeviceQuery) (mesh.Device, error) {
	return mesh.Device{NodeID: e2eDeviceID, Name: q.Hostname, Hostname: q.Hostname, Addresses: []string{e2eDeviceIP}, Tags: append([]string(nil), q.Tags...), Online: true}, nil
}

type e2eDoctorRemote struct {
	secrets []string
}

func (r e2eDoctorRemote) Run(_ context.Context, user, _ string, script string) (string, error) {
	failure := os.Getenv(e2eDoctorPackageFailureEnv)
	if (failure == e2eDoctorPackageFailureShort || failure == e2eDoctorPackageFailureBoundary) && script == bootstraptools.InstallScriptForUser(user) {
		out := strings.Join(r.secrets, "\n") + "\n" + strings.Repeat(e2eDoctorNoise, e2eDoctorNoiseLines) +
			strings.Join(r.secrets, "\n") + "\n" + e2eDoctorRepairCause
		return out, errors.New(e2eDoctorRepairErrorPrefix + out)
	}
	return e2eRemoteSuccessEvidence, nil
}

func (r e2eDoctorRemote) RunBatch(_ context.Context, user, _ string, commands []remote.BatchCommand) ([]remote.BatchResult, error) {
	results := make([]remote.BatchResult, len(commands))
	for i := range results {
		results[i].Output = e2eRemoteSuccessEvidence
	}
	failure := os.Getenv(e2eDoctorPackageFailureEnv)
	if failure != e2eDoctorPackageFailureShort && failure != e2eDoctorPackageFailureBoundary {
		return results, nil
	}
	var packageCommand string
	for _, check := range bootstraptools.Checks(user) {
		if check.Name == bootstraptools.ManagedPackageCheckName {
			packageCommand = check.Command
			break
		}
	}
	for i, command := range commands {
		if command.Script != packageCommand {
			continue
		}
		err := fmt.Errorf("remote batch command %d failed with status %d", i, e2eDoctorPackageFailureStatus)
		out := e2eDoctorPackageEvidence + "\n"
		if failure == e2eDoctorPackageFailureBoundary {
			// A split credential exposes truncation before redaction even when the complete token disappears.
			secret := r.secrets[0]
			padding := e2eDoctorEvidenceHeadBytes - len(err.Error()+"\n"+out) - len(secret)/2
			out += strings.Repeat(e2eDoctorPadding, padding) + secret + "\n" + strings.Repeat(e2eDoctorNoise, e2eDoctorNoiseLines)
		}
		out += strings.Join(r.secrets, "\n")
		results[i] = remote.BatchResult{Output: out, Err: err}
	}
	return results, nil
}

type e2eCleanupTailscale struct {
	state       state.State
	deviceReads int
}

func (c *e2eCleanupTailscale) Devices(context.Context) ([]mesh.Device, error) {
	c.deviceReads++
	failure := os.Getenv(e2eDeleteCleanupFailureEnv)
	if failure == e2eDeleteCleanupFailPreflight || failure == e2eDeleteCleanupFailAfter && c.deviceReads > 1 {
		return nil, &httpjson.StatusError{
			Method:     http.MethodGet,
			Path:       e2eDeleteCleanupDevicesPath,
			Status:     e2eDeleteCleanupUnauthorized,
			StatusCode: http.StatusUnauthorized,
			Body:       e2eDeleteCleanupUnauthorizedBody,
		}
	}
	return []mesh.Device{{
		ID:       c.state.Tailscale.NodeID,
		NodeID:   c.state.Tailscale.NodeID,
		Name:     c.state.Tailscale.Name,
		Hostname: c.state.Tailscale.Name,
		Tags:     append([]string(nil), c.state.Tailscale.Tags...),
	}}, nil
}

func (c *e2eCleanupTailscale) AuthKeys(context.Context) ([]mesh.AuthKey, error) {
	if c.state.Tailscale.AuthKeyID == "" {
		return nil, nil
	}
	return []mesh.AuthKey{{
		ID:          c.state.Tailscale.AuthKeyID,
		Description: "serverpro bootstrap",
		Capabilities: mesh.AuthKeyCapabilities{Devices: mesh.AuthKeyDeviceCapabilities{Create: mesh.AuthKeyCreateCapabilities{
			Tags: append([]string(nil), c.state.Tailscale.Tags...),
		}}},
	}}, nil
}

func (*e2eCleanupTailscale) DeleteDevice(_ context.Context, id string) error {
	return appendE2EAudit(e2eDeleteDeviceEvent, id)
}

func (*e2eCleanupTailscale) DeleteAuthKey(_ context.Context, id string) error {
	return appendE2EAudit(e2eDeleteAuthKeyEvent, id)
}

func appendE2EAudit(event, id string) error {
	path := os.Getenv(e2eAuditEnv)
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, e2eCheckpointFileMode)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "%s:%s\n", event, id); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
