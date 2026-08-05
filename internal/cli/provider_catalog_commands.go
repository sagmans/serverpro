package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/assagman/serverpro/internal/compute"
)

type providerRow struct {
	Name string `json:"name"`
}

type providerStatusRow struct {
	Name         string               `json:"name"`
	Capabilities compute.Capabilities `json:"capabilities"`
}

func (a *app) runProviderList() error {
	providers := a.providerRegistry().List()
	rows := make([]providerRow, 0, len(providers))
	for _, provider := range providers {
		rows = append(rows, providerRow{Name: string(provider.Name())})
	}
	return writeJSON(a.stdout, rows)
}

func (a *app) runProviderStatus(ctx context.Context, name string) error {
	provider, err := a.resolveProvider(name)
	if err != nil {
		return err
	}
	capabilities := provider.Capabilities(ctx)
	row := providerStatusRow{Name: string(provider.Name()), Capabilities: capabilities}
	return writeJSON(a.stdout, row)
}

func (a *app) runProviderDoctor(ctx context.Context, name string) error {
	provider, err := a.resolveProvider(name)
	if err != nil {
		return err
	}
	accountRef, err := a.ephemeralComputeAccount(provider)
	if err != nil {
		return err
	}
	diagnostics := provider.Doctor(ctx, accountRef)
	if err := writeJSON(a.stdout, diagnostics); err != nil {
		return err
	}
	return diagnostics.Err()
}

func (a *app) runCatalog(ctx context.Context, kind, location string) error {
	if a.provider == "" {
		return fmt.Errorf("--provider required for catalog %s", kind)
	}
	provider, err := a.resolveProvider(a.provider)
	if err != nil {
		return err
	}
	accountRef, err := a.ephemeralComputeAccount(provider)
	if err != nil {
		return err
	}
	catalog, diagnostics := provider.Catalog(ctx, compute.CatalogQuery{Account: accountRef, Location: location})
	if !diagnostics.Passed() {
		return diagnostics.Err()
	}
	switch kind {
	case "locations":
		return writeJSON(a.stdout, catalog.Locations)
	case "sizes":
		return writeJSON(a.stdout, catalog.Sizes)
	case "images":
		return writeJSON(a.stdout, catalog.Images)
	default:
		return fmt.Errorf("unsupported catalog %q", kind)
	}
}

func (a *app) resolveProvider(name string) (compute.Provider, error) {
	if name == "" {
		return nil, fmt.Errorf("provider required")
	}
	provider, ok := a.providerRegistry().Get(compute.ProviderName(name))
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return provider, nil
}

func (a *app) ephemeralComputeAccount(provider compute.Provider) (compute.Account, error) {
	if account, ok := a.cachedEphemeralComputeAccount(provider.Name()); ok {
		return account, nil
	}
	token := os.Getenv("SERVERPRO_SERVER_PROVIDER_TOKEN")
	if token == "" {
		token = os.Getenv("SERVER_PROVIDER_TOKEN")
	}
	if token == "" {
		if a.nonInteractive {
			return compute.Account{}, fmt.Errorf("SERVERPRO_SERVER_PROVIDER_TOKEN required for non-interactive provider API access")
		}
		var err error
		token, err = a.promptSecret("server provider API token")
		if err != nil {
			return compute.Account{}, err
		}
	}
	a.addRuntimeSecret(token)
	account := compute.Account{Name: "ephemeral", Provider: provider.Name(), Token: token, Scope: "ephemeral"}
	a.cacheEphemeralComputeAccount(account)
	return account, nil
}

func (a *app) cachedEphemeralComputeAccount(provider compute.ProviderName) (compute.Account, bool) {
	if a.ephemeralAccounts == nil {
		return compute.Account{}, false
	}
	account, ok := a.ephemeralAccounts[provider]
	return account, ok && account.Token != ""
}

func (a *app) cacheEphemeralComputeAccount(account compute.Account) {
	if account.Token == "" {
		return
	}
	if a.ephemeralAccounts == nil {
		a.ephemeralAccounts = map[compute.ProviderName]compute.Account{}
	}
	a.ephemeralAccounts[account.Provider] = account
}
