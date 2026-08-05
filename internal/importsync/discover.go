// Package importsync rebuilds local serverpro state from cloud ownership labels.
// WHY: local config/state is the operational SoT; discover/import recover control after local loss.
package importsync

import (
	"context"
	"fmt"
	"sort"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/state"
)

// Candidate is one provider resource that can be reattached as a managed server.
type Candidate struct {
	Provider   compute.ProviderName `json:"provider"`
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Namespace  string               `json:"namespace"`
	Server     string               `json:"server"`
	PublicIPv4 string               `json:"public_ipv4,omitempty"`
	Location   string               `json:"location,omitempty"`
	Size       string               `json:"size,omitempty"`
	Image      string               `json:"image,omitempty"`
	LabelsOK   bool                 `json:"labels_ok"`
	LocalState string               `json:"local_state"`
	Record     compute.ServerRecord `json:"-"`
}

// DiscoverFilter limits which labeled resources are returned.
type DiscoverFilter struct {
	Namespace        string
	Server           string
	ProviderID       string
	IncludeUnmanaged bool
}

// Discover lists provider inventory and keeps serverpro-owned candidates by default.
func Discover(ctx context.Context, provider compute.Provider, account compute.Account, filter DiscoverFilter) ([]Candidate, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider required")
	}
	if account.Token == "" {
		return nil, fmt.Errorf("provider credential missing")
	}
	records, diagnostics := provider.List(ctx, compute.ListServersQuery{Account: account})
	if err := diagnostics.Err(); err != nil {
		return nil, err
	}
	reg, err := state.LoadRegistry(stateRegistryPath())
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(records))
	for _, record := range records {
		candidate, ok := candidateFromRecord(provider.Name(), record, filter, reg)
		if !ok {
			continue
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].Server != out[j].Server {
			return out[i].Server < out[j].Server
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func candidateFromRecord(providerName compute.ProviderName, record compute.ServerRecord, filter DiscoverFilter, reg state.Registry) (Candidate, bool) {
	if record.Provider == "" {
		record.Provider = providerName
	}
	namespace, server, managed := ownership.OwnershipFromLabels(record.Labels)
	if !managed {
		if !filter.IncludeUnmanaged {
			return Candidate{}, false
		}
		namespace, server = record.Namespace, record.Server
	}
	if filter.Namespace != "" && namespace != filter.Namespace {
		return Candidate{}, false
	}
	if filter.Server != "" && server != filter.Server {
		return Candidate{}, false
	}
	if filter.ProviderID != "" && record.ID != filter.ProviderID {
		return Candidate{}, false
	}
	if !managed && !filter.IncludeUnmanaged {
		return Candidate{}, false
	}
	record.Namespace = namespace
	record.Server = server
	return Candidate{
		Provider:   providerName,
		ID:         record.ID,
		Name:       record.Name,
		Namespace:  namespace,
		Server:     server,
		PublicIPv4: record.PublicIPv4,
		Location:   record.Location,
		Size:       record.Size,
		Image:      record.Image,
		LabelsOK:   managed,
		LocalState: localStateStatus(reg, namespace, server),
		Record:     record,
	}, true
}

func localStateStatus(reg state.Registry, namespace, server string) string {
	if namespace == "" || server == "" {
		return "missing"
	}
	if _, ok := reg.Find(namespace, server); ok {
		return "present"
	}
	return "missing"
}

// stateRegistryPath is replaced in tests when needed.
var stateRegistryPath = func() string {
	return defaultRegistryPath()
}
