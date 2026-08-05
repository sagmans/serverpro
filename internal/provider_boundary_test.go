package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	digitalOceanAdapterImport = "github.com/sagmans/serverpro/internal/provider/digitalocean"
	hetznerAdapterImport      = "github.com/sagmans/serverpro/internal/provider/hetzner"
	vultrAdapterImport        = "github.com/sagmans/serverpro/internal/provider/vultr"
	cloudflareAdapterImport   = "github.com/sagmans/serverpro/internal/provider/cloudflare"
	tailscaleAdapterImport    = "github.com/sagmans/serverpro/internal/provider/tailscale"
)

var providerAdapterImportAllowlist = map[string]bool{
	filePathClean("cli/e2e_provider_registry.go"): true,
	filePathClean("cli/provider_registry.go"):     true,
	filePathClean("cli/services.go"):              true,
}

var providerSpecificTermAllowlist = map[string]bool{
	filePathClean("cli/e2e_provider_registry.go"): true,
	filePathClean("cli/provider_registry.go"):     true,
}

func TestGenericPackagesDoNotImportProviderAdapters(t *testing.T) {
	for _, tc := range []struct {
		name       string
		importPath string
		adapterDir string
	}{
		{name: "digitalocean", importPath: digitalOceanAdapterImport, adapterDir: "provider/digitalocean"},
		{name: "hetzner", importPath: hetznerAdapterImport, adapterDir: "provider/hetzner"},
		{name: "vultr", importPath: vultrAdapterImport, adapterDir: "provider/vultr"},
		{name: "cloudflare", importPath: cloudflareAdapterImport, adapterDir: "provider/cloudflare"},
		{name: "tailscale", importPath: tailscaleAdapterImport, adapterDir: "provider/tailscale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return err
				}
				clean := filePathClean(path)
				if strings.HasPrefix(clean, filePathClean(tc.adapterDir)+string(os.PathSeparator)) || providerAdapterImportAllowlist[clean] {
					return nil
				}
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(string(content), tc.importPath) {
					t.Errorf("%s imports %s adapter outside adapter registration", clean, tc.name)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGenericProductionCodeDoesNotUseProviderSpecificTerms(t *testing.T) {
	terms := []string{"DigitalOcean", "digitalocean", "DIGITALOCEAN", "Hetzner", "hetzner", "HETZNER", "Vultr", "vultr", "VULTR"}
	assertProductionCodeExcludesTerms(t, terms, providerAdapterDirs(), providerSpecificTermAllowlist)
}

func TestProductionCodeDoesNotUseLegacyProjectFlagOutput(t *testing.T) {
	assertProductionCodeExcludesTerms(t, []string{"--project", "project="}, nil, nil)
}

func assertProductionCodeExcludesTerms(t *testing.T, terms []string, adapterDirs []string, allowlist map[string]bool) {
	t.Helper()
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		clean := filePathClean(path)
		for _, dir := range adapterDirs {
			if strings.HasPrefix(clean, dir+string(os.PathSeparator)) {
				return nil
			}
		}
		if allowlist[clean] {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		for _, term := range terms {
			if strings.Contains(text, term) {
				t.Errorf("%s contains forbidden term %q", clean, term)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func providerAdapterDirs() []string {
	return []string{
		filePathClean("provider/digitalocean"),
		filePathClean("provider/hetzner"),
		filePathClean("provider/vultr"),
		filePathClean("provider/cloudflare"),
		filePathClean("provider/tailscale"),
	}
}

func filePathClean(path string) string {
	return filepath.Clean(path)
}
