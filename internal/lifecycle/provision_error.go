package lifecycle

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/state"
)

type ProvisionPhase string

const (
	ProvisionPhaseInitialize                ProvisionPhase = "initialize"
	ProvisionPhaseTailscalePolicy           ProvisionPhase = "tailscale_policy"
	ProvisionPhaseCloudflareTunnel          ProvisionPhase = "cloudflare_tunnel"
	ProvisionPhaseTailscaleAuthKey          ProvisionPhase = "tailscale_auth_key"
	ProvisionPhaseTailscalePolicyValidation ProvisionPhase = "tailscale_policy_validation"
	ProvisionPhaseBootstrapRender           ProvisionPhase = "bootstrap_render"
	ProvisionPhaseCompute                   ProvisionPhase = "compute"
	ProvisionPhaseTailscaleDevice           ProvisionPhase = "tailscale_device"
	ProvisionPhaseRemoteReady               ProvisionPhase = "remote_ready"
	ProvisionPhaseRemoteBootstrap           ProvisionPhase = "remote_bootstrap"
	ProvisionPhaseComplete                  ProvisionPhase = "complete"
)

// ProvisionResources snapshots non-secret identifiers needed to reconcile a
// failed create even when its latest state checkpoint could not be published.
type ProvisionResources struct {
	ComputeID          string
	ManagedResources   []compute.ManagedResourceRef
	CloudflareTunnelID string
	TailscaleAuthKeyID string
	TailscaleNodeID    string
}

// ProvisionError gives callers a stable failed phase and recoverable resource
// evidence while preserving the original error for retry classification.
type ProvisionError struct {
	Phase     ProvisionPhase
	Resources ProvisionResources
	cause     error
}

func (e *ProvisionError) Error() string {
	resources := provisionResourceLabels(e.Resources)
	if len(resources) == 0 {
		return fmt.Sprintf("provision %s failed: %v", e.Phase, e.cause)
	}
	return fmt.Sprintf("provision %s failed (%s): %v", e.Phase, strings.Join(resources, ", "), e.cause)
}

func (e *ProvisionError) Unwrap() error { return e.cause }

func newProvisionError(phase ProvisionPhase, st state.State, cause error) error {
	if cause == nil {
		return nil
	}
	resources := append([]compute.ManagedResourceRef(nil), st.Compute.ManagedResources...)
	return &ProvisionError{
		Phase: phase,
		Resources: ProvisionResources{
			ComputeID:          st.Compute.ID,
			ManagedResources:   resources,
			CloudflareTunnelID: st.Cloudflare.TunnelID,
			TailscaleAuthKeyID: st.Tailscale.AuthKeyID,
			TailscaleNodeID:    st.Tailscale.NodeID,
		},
		cause: cause,
	}
}

func provisionResourceLabels(resources ProvisionResources) []string {
	var labels []string
	if resources.CloudflareTunnelID != "" {
		labels = append(labels, "cloudflare_tunnel="+resources.CloudflareTunnelID)
	}
	if resources.TailscaleAuthKeyID != "" {
		labels = append(labels, "tailscale_auth_key="+resources.TailscaleAuthKeyID)
	}
	if resources.ComputeID != "" {
		labels = append(labels, "compute="+resources.ComputeID)
	}
	if resources.TailscaleNodeID != "" {
		labels = append(labels, "tailscale_device="+resources.TailscaleNodeID)
	}
	managed := append([]compute.ManagedResourceRef(nil), resources.ManagedResources...)
	sort.Slice(managed, func(i, j int) bool { return managed[i].Kind < managed[j].Kind })
	for _, resource := range managed {
		if resource.ID != "" {
			labels = append(labels, string(resource.Kind)+"="+resource.ID)
		}
	}
	return labels
}
