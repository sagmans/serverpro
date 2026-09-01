package cli

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/state"
)

type ingressMutationRow struct {
	Status   string `json:"status"`
	Action   string `json:"action,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
	Server   string `json:"server"`
	Type     string `json:"type,omitempty"`
	Hostname string `json:"hostname"`
}

func (a *app) runIngressList(_ context.Context, name string) error {
	_, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	return writeJSON(a.stdout, st.Ingress)
}

func (a *app) runIngressAdd(ctx context.Context, name, ingressType, hostname string) error {
	stPath, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	if ingressType == "" {
		return requiredFlagError("serverpro ingress add", "type", "")
	}
	if hostname == "" {
		return requiredFlagError("serverpro ingress add", "hostname", "")
	}
	adapter, ok := a.resolveIngressAdapter(ingress.Type(ingressType))
	if !ok {
		return fmt.Errorf("unsupported ingress type %q", ingressType)
	}
	for _, existing := range st.Ingress {
		if existing.Hostname == hostname {
			return fmt.Errorf("ingress hostname %q already exists", hostname)
		}
	}
	if a.dryRun {
		row := ingressMutationRow{Status: "planned", Action: "add", DryRun: true, Server: name, Type: ingressType, Hostname: hostname}
		return writeJSON(a.stdout, row)
	}
	route, err := adapter.Add(ctx, ingress.Route{Hostname: hostname, Target: st.Tailscale.Name})
	if err != nil {
		return err
	}
	entry := state.IngressState{Type: string(route.Type), Hostname: route.Hostname, Target: route.Target, Status: route.Status}
	if err := state.Update(config.Expand(stPath), func(current *state.State) error {
		for _, existing := range current.Ingress {
			if existing.Hostname == hostname {
				return fmt.Errorf("ingress hostname %q already exists", hostname)
			}
		}
		current.Ingress = append(current.Ingress, entry)
		return nil
	}); err != nil {
		return err
	}
	row := ingressMutationRow{Status: "added", Server: name, Type: ingressType, Hostname: hostname}
	return writeJSON(a.stdout, row)
}

func (a *app) resolveIngressAdapter(ingressType ingress.Type) (ingress.Adapter, bool) {
	if a.ingressAdapters != nil {
		adapter, ok := a.ingressAdapters[ingressType]
		return adapter, ok
	}
	if ingressType == ingress.CloudflareTunnel {
		return ingress.CloudflareTunnelAdapter{}, true
	}
	return nil, false
}

func (a *app) runIngressRemove(ctx context.Context, name, hostname string) error {
	stPath, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	if hostname == "" {
		return requiredFlagError("serverpro ingress remove", "hostname", "")
	}
	removed := false
	for _, item := range st.Ingress {
		if item.Hostname != hostname {
			continue
		}
		// Remove every matching route from its provider so duplicate hostnames
		// (across types) cannot orphan resources. The add path rejects duplicate
		// hostnames today, but this stays correct if that invariant loosens.
		adapter, ok := a.resolveIngressAdapter(ingress.Type(item.Type))
		if !ok {
			return fmt.Errorf("unsupported ingress type %q", item.Type)
		}
		if a.dryRun {
			removed = true
			continue
		}
		route := ingress.Route{Type: ingress.Type(item.Type), Hostname: item.Hostname, Target: item.Target, Status: item.Status}
		if err := adapter.Remove(ctx, route); err != nil {
			return err
		}
		removed = true
	}
	if !removed {
		return fmt.Errorf("ingress hostname %q not found", hostname)
	}
	if a.dryRun {
		row := ingressMutationRow{Status: "planned", Action: "remove", DryRun: true, Server: name, Hostname: hostname}
		return writeJSON(a.stdout, row)
	}
	if err := state.Update(config.Expand(stPath), func(current *state.State) error {
		kept := current.Ingress[:0]
		for _, item := range current.Ingress {
			if item.Hostname != hostname {
				kept = append(kept, item)
			}
		}
		current.Ingress = kept
		return nil
	}); err != nil {
		return err
	}
	row := ingressMutationRow{Status: "removed", Server: name, Hostname: hostname}
	return writeJSON(a.stdout, row)
}
