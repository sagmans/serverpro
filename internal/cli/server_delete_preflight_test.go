package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	deletePreflightNodeID             = "node-preflight"
	deletePreflightDeviceName         = "demoapp-webapp"
	deletePreflightTunnelID           = "tunnel-preflight"
	deletePreflightAuthKeyID          = "auth-key-preflight"
	deletePreflightDevicesPath        = "/tailnet/-/devices"
	deletePreflightUnauthorizedStatus = "401 Unauthorized"
	deletePreflightUnauthorizedBody   = `{"message":"API token invalid"}`
	deletePreflightDeviceReadError    = "device read failed"
	deletePreflightTunnelReadError    = "tunnel read failed"
	deletePreflightAuthKeyReadError   = "auth key read failed"
	deletePartialExpectedStatus       = "partial"
	deletePartialExpectedAction       = "delete"
	deletePartialExpectedStage        = "external_cleanup"
	deletePartialNextActionFragment   = "rerun the same delete command"
	deletePartialSecret               = "delete-partial-secret-token"
	deletePartialSecretErrorFormat    = "cleanup failed with credential %s: %w"
)

func TestServerDeleteRejectsExternalPreflightBeforeProviderMutation(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale.NodeID = deletePreflightNodeID
	st.Tailscale.Name = deletePreflightDeviceName
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	provider := &powerDeleteFakeProvider{}
	client := &sequencedDeleteTailscale{deviceErrors: []error{deletePreflightUnauthorizedError()}}
	var out bytes.Buffer
	a := &app{
		stdout:    &out,
		stderr:    io.Discard,
		project:   "demoapp",
		provider:  "hetzner",
		yes:       true,
		providers: providerRegistryForPower(t, provider),
		services: serviceHooks{cleanupClients: func(serverDeleteCleanup) serverCleanupClients {
			return serverCleanupClients{Tailscale: client}
		}},
	}

	err = a.runServerDelete(context.Background(), "webapp")
	if err == nil || !strings.Contains(err.Error(), "before compute deletion") {
		t.Fatalf("preflight error = %v", err)
	}
	if provider.deleted {
		t.Fatal("compute provider delete ran after cleanup preflight failure")
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected preflight stdout: %s", out.String())
	}
	if exists, existsErr := state.Exists(config.Expand(stPath)); existsErr != nil || !exists {
		t.Fatalf("state retained = %t, error = %v", exists, existsErr)
	}
	registry, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Find("demoapp", "webapp"); !exists {
		t.Fatal("registry authority removed after cleanup preflight failure")
	}
}

func TestServerDeleteReportsPartialResultAfterComputeDeletion(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.ExampleServer("demoapp", "webapp")
	creds, err := credentials.LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	creds.Tailscale = deletePartialSecret
	if err := credentials.Save(cfg, creds); err != nil {
		t.Fatal(err)
	}
	st.Tailscale.NodeID = deletePreflightNodeID
	st.Tailscale.Name = deletePreflightDeviceName
	st.Tailscale.Tags = append([]string(nil), cfg.Access.Tailscale.Tags...)
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	client := &sequencedDeleteTailscale{
		devices: []mesh.Device{{
			ID:       deletePreflightNodeID,
			NodeID:   deletePreflightNodeID,
			Name:     deletePreflightDeviceName + ".tail.ts.net",
			Hostname: deletePreflightDeviceName,
			Tags:     append([]string(nil), st.Tailscale.Tags...),
		}},
		deviceErrors: []error{nil, fmt.Errorf(deletePartialSecretErrorFormat, deletePartialSecret, deletePreflightUnauthorizedError())},
	}
	provider := &powerDeleteFakeProvider{}
	var out bytes.Buffer
	a := &app{
		stdout:    &out,
		stderr:    io.Discard,
		project:   "demoapp",
		provider:  "hetzner",
		yes:       true,
		providers: providerRegistryForPower(t, provider),
		services: serviceHooks{cleanupClients: func(serverDeleteCleanup) serverCleanupClients {
			return serverCleanupClients{Tailscale: client}
		}},
	}

	err = a.runServerDelete(context.Background(), "webapp")
	if err == nil {
		t.Fatal("partial delete returned success")
	}
	if !provider.deleted || provider.deletedCount != 1 {
		t.Fatalf("provider delete count = %d", provider.deletedCount)
	}
	var row struct {
		Status                   string                         `json:"status"`
		Action                   string                         `json:"action"`
		FailureStage             string                         `json:"failure_stage"`
		ComputeDeleted           bool                           `json:"compute_deleted"`
		LocalStateRetained       bool                           `json:"local_state_retained"`
		Retryable                bool                           `json:"retryable"`
		Error                    string                         `json:"error"`
		NextAction               string                         `json:"next_action"`
		RemainingExternalCleanup []serverDeleteExternalResource `json:"remaining_external_cleanup"`
	}
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("partial output is not JSON: %v\n%s", err, out.String())
	}
	if row.Status != deletePartialExpectedStatus || row.Action != deletePartialExpectedAction || row.FailureStage != deletePartialExpectedStage {
		t.Fatalf("partial identity = %+v", row)
	}
	if !row.ComputeDeleted || !row.LocalStateRetained || !row.Retryable {
		t.Fatalf("partial recovery flags = %+v", row)
	}
	if !strings.Contains(row.Error, deletePreflightUnauthorizedStatus) || !strings.Contains(row.NextAction, deletePartialNextActionFragment) {
		t.Fatalf("partial guidance = %+v", row)
	}
	if strings.Contains(row.Error, deletePartialSecret) || strings.Contains(err.Error(), deletePartialSecret) {
		t.Fatalf("partial failure exposed credential: row=%q error=%q", row.Error, err)
	}
	if len(row.RemainingExternalCleanup) != 1 || row.RemainingExternalCleanup[0].ID != deletePreflightNodeID {
		t.Fatalf("remaining cleanup = %+v", row.RemainingExternalCleanup)
	}
	if exists, existsErr := state.Exists(config.Expand(stPath)); existsErr != nil || !exists {
		t.Fatalf("state retained = %t, error = %v", exists, existsErr)
	}
	registry, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Find("demoapp", "webapp"); !exists {
		t.Fatal("registry authority removed after partial delete")
	}
}

func TestPreflightTrackedExternalResourcesRejectsEveryReadFailure(t *testing.T) {
	deviceReadErr := errors.New(deletePreflightDeviceReadError)
	tunnelReadErr := errors.New(deletePreflightTunnelReadError)
	authKeyReadErr := errors.New(deletePreflightAuthKeyReadError)
	tests := []struct {
		name    string
		state   state.State
		clients serverCleanupClients
		wantErr error
	}{
		{
			name:  "tailscale device",
			state: state.State{Tailscale: state.TailscaleState{NodeID: deletePreflightNodeID}},
			clients: serverCleanupClients{Tailscale: &recordingPreflightTailscale{
				deviceErr: deviceReadErr,
			}},
			wantErr: deviceReadErr,
		},
		{
			name: "cloudflare tunnel",
			state: state.State{Cloudflare: state.CloudflareState{
				TunnelID:   deletePreflightTunnelID,
				Provenance: state.CloudflareTunnelCreated,
			}},
			clients: serverCleanupClients{Cloudflare: &recordingPreflightCloudflare{
				getErr: tunnelReadErr,
			}},
			wantErr: tunnelReadErr,
		},
		{
			name:  "tailscale auth key",
			state: state.State{Tailscale: state.TailscaleState{AuthKeyID: deletePreflightAuthKeyID}},
			clients: serverCleanupClients{Tailscale: &recordingPreflightTailscale{
				authKeyErr: authKeyReadErr,
			}},
			wantErr: authKeyReadErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanup := serverDeleteCleanup{Config: config.ExampleServer("demoapp", "webapp"), State: test.state}
			err := preflightTrackedExternalResources(context.Background(), cleanup, test.clients)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("preflight error = %v, want %v", err, test.wantErr)
			}
			if test.clients.Tailscale != nil {
				client := test.clients.Tailscale.(*recordingPreflightTailscale)
				if client.deletedDevice != "" || client.deletedAuthKey != "" {
					t.Fatalf("preflight mutated Tailscale: %+v", client)
				}
			}
			if test.clients.Cloudflare != nil {
				client := test.clients.Cloudflare.(*recordingPreflightCloudflare)
				if client.deletedTunnel != "" {
					t.Fatalf("preflight mutated Cloudflare: %+v", client)
				}
			}
		})
	}
}

func TestPreflightTrackedExternalResourcesReadsAllTrackedResourcesWithoutMutation(t *testing.T) {
	st := cleanupTestState()
	tailscaleClient := &recordingPreflightTailscale{
		devices:  cleanupLiveDevices(),
		authKeys: cleanupLiveAuthKeys(),
	}
	cloudflareClient := &recordingPreflightCloudflare{tunnel: cleanupLiveTunnel()}
	cleanup := serverDeleteCleanup{Config: config.ExampleServer("demoapp", "webapp"), State: st}

	if err := preflightTrackedExternalResources(context.Background(), cleanup, serverCleanupClients{
		Tailscale:  tailscaleClient,
		Cloudflare: cloudflareClient,
	}); err != nil {
		t.Fatal(err)
	}
	if tailscaleClient.deviceReads != 1 || tailscaleClient.authKeyReads != 1 || cloudflareClient.getReads != 1 {
		t.Fatalf("preflight reads tailscale=%+v cloudflare=%+v", tailscaleClient, cloudflareClient)
	}
	if tailscaleClient.deletedDevice != "" || tailscaleClient.deletedAuthKey != "" || cloudflareClient.deletedTunnel != "" {
		t.Fatalf("preflight mutated resources tailscale=%+v cloudflare=%+v", tailscaleClient, cloudflareClient)
	}
}

func successfulDeletePreflightClients(cleanup serverDeleteCleanup) serverCleanupClients {
	st := cleanup.State
	deviceName := st.Tailscale.Name
	if deviceName == "" {
		deviceName = cleanup.Config.Compute.Name
	}
	tunnelName := st.Cloudflare.Name
	if tunnelName == "" {
		tunnelName = cleanup.Config.Cloudflare.Tunnel.Name
	}
	tailscaleTags := st.Tailscale.Tags
	if len(tailscaleTags) == 0 {
		tailscaleTags = cleanup.Config.Access.Tailscale.Tags
	}
	tailscaleClient := &recordingPreflightTailscale{}
	if st.Tailscale.NodeID != "" {
		tailscaleClient.devices = []mesh.Device{{
			ID:       st.Tailscale.NodeID,
			NodeID:   st.Tailscale.NodeID,
			Name:     deviceName + ".tail.ts.net",
			Hostname: deviceName,
			Tags:     append([]string(nil), tailscaleTags...),
		}}
	}
	if st.Tailscale.AuthKeyID != "" {
		tailscaleClient.authKeys = []mesh.AuthKey{{
			ID:          st.Tailscale.AuthKeyID,
			Description: "serverpro bootstrap",
			Capabilities: mesh.AuthKeyCapabilities{Devices: mesh.AuthKeyDeviceCapabilities{Create: mesh.AuthKeyCreateCapabilities{
				Tags: append([]string(nil), tailscaleTags...),
			}}},
		}}
	}
	return serverCleanupClients{
		Tailscale: tailscaleClient,
		Cloudflare: &recordingPreflightCloudflare{tunnel: ingress.Tunnel{
			ID:   st.Cloudflare.TunnelID,
			Name: tunnelName,
		}},
	}
}

func deletePreflightUnauthorizedError() error {
	return &httpjson.StatusError{
		Method:     http.MethodGet,
		Path:       deletePreflightDevicesPath,
		Status:     deletePreflightUnauthorizedStatus,
		StatusCode: http.StatusUnauthorized,
		Body:       deletePreflightUnauthorizedBody,
	}
}

type recordingPreflightTailscale struct {
	devices        []mesh.Device
	authKeys       []mesh.AuthKey
	deviceErr      error
	authKeyErr     error
	deviceReads    int
	authKeyReads   int
	deletedDevice  string
	deletedAuthKey string
}

func (c *recordingPreflightTailscale) Devices(context.Context) ([]mesh.Device, error) {
	c.deviceReads++
	return append([]mesh.Device(nil), c.devices...), c.deviceErr
}

func (c *recordingPreflightTailscale) AuthKeys(context.Context) ([]mesh.AuthKey, error) {
	c.authKeyReads++
	return append([]mesh.AuthKey(nil), c.authKeys...), c.authKeyErr
}

func (c *recordingPreflightTailscale) DeleteDevice(_ context.Context, id string) error {
	c.deletedDevice = id
	return nil
}

func (c *recordingPreflightTailscale) DeleteAuthKey(_ context.Context, id string) error {
	c.deletedAuthKey = id
	return nil
}

type recordingPreflightCloudflare struct {
	tunnel        ingress.Tunnel
	getErr        error
	getReads      int
	deletedTunnel string
}

func (c *recordingPreflightCloudflare) GetTunnel(context.Context, string) (ingress.Tunnel, error) {
	c.getReads++
	return c.tunnel, c.getErr
}

func (c *recordingPreflightCloudflare) DeleteTunnel(_ context.Context, id string) error {
	c.deletedTunnel = id
	return nil
}

type sequencedDeleteTailscale struct {
	devices        []mesh.Device
	deviceErrors   []error
	deviceCalls    int
	deletedDevice  string
	deletedAuthKey string
}

func (c *sequencedDeleteTailscale) Devices(context.Context) ([]mesh.Device, error) {
	call := c.deviceCalls
	c.deviceCalls++
	if call < len(c.deviceErrors) && c.deviceErrors[call] != nil {
		return nil, c.deviceErrors[call]
	}
	return append([]mesh.Device(nil), c.devices...), nil
}

func (c *sequencedDeleteTailscale) AuthKeys(context.Context) ([]mesh.AuthKey, error) {
	return nil, nil
}

func (c *sequencedDeleteTailscale) DeleteDevice(_ context.Context, id string) error {
	c.deletedDevice = id
	return nil
}

func (c *sequencedDeleteTailscale) DeleteAuthKey(_ context.Context, id string) error {
	c.deletedAuthKey = id
	return nil
}
