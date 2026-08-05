package importsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/privatefile"
	"github.com/sagmans/serverpro/internal/state"
)

type importWriteStage string

const (
	importWriteMarker      importWriteStage = "marker"
	importWriteConfig      importWriteStage = "config"
	importWriteCredentials importWriteStage = "credentials"
	importWriteState       importWriteStage = "state"
	importWriteRegistry    importWriteStage = "registry"
)

const importMarkerSchemaVersion = 1

type importMarker struct {
	SchemaVersion int    `json:"schema_version"`
	Namespace     string `json:"namespace"`
	Server        string `json:"server"`
	Provider      string `json:"provider"`
	ProviderID    string `json:"provider_id"`
}

func importMarkerPath(statePath string) string {
	return statePath + ".import.json"
}

func importIsResumable(statePath string, candidate Candidate) (bool, error) {
	marker, found, err := loadImportMarker(importMarkerPath(statePath))
	if err != nil || !found {
		return found, err
	}
	want := markerForCandidate(candidate)
	if marker != want {
		return false, fmt.Errorf("import marker identity mismatch for %s/%s", candidate.Namespace, candidate.Server)
	}
	return true, nil
}

func markerForCandidate(candidate Candidate) importMarker {
	return importMarker{
		SchemaVersion: importMarkerSchemaVersion,
		Namespace:     candidate.Namespace,
		Server:        candidate.Server,
		Provider:      string(candidate.Provider),
		ProviderID:    candidate.ID,
	}
}

func markerForState(st state.State) importMarker {
	return importMarker{
		SchemaVersion: importMarkerSchemaVersion,
		Namespace:     st.Namespace,
		Server:        st.Server,
		Provider:      st.Compute.Provider,
		ProviderID:    st.Compute.ID,
	}
}

func loadImportMarker(path string) (importMarker, bool, error) {
	var marker importMarker
	if err := privatefile.ReadJSON(path, &marker, "import marker"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return importMarker{}, false, nil
		}
		return importMarker{}, false, err
	}
	return marker, true, nil
}

func writeImportArtifacts(cfg config.Config, st state.State, cfgPath, stPath string, opts ImportOptions) error {
	markerPath := importMarkerPath(stPath)
	marker := markerForState(st)
	if existing, found, err := loadImportMarker(markerPath); err != nil {
		return err
	} else if found && existing != marker {
		return fmt.Errorf("import marker identity mismatch for %s/%s", st.Namespace, st.Server)
	}
	if err := runImportWrite(opts, importWriteMarker, func() error {
		return privatefile.AtomicWriteJSON(markerPath, marker, privatefile.WriteOptions{TempPattern: ".import-*.tmp", Sync: true})
	}); err != nil {
		return err
	}
	if err := runImportWrite(opts, importWriteConfig, func() error {
		if opts.checkConfigSource {
			return config.SaveIfUnchanged(cfgPath, cfg, opts.existingConfigSource, opts.existingConfig != nil)
		}
		return config.Save(cfgPath, cfg)
	}); err != nil {
		return err
	}
	creds := credentials.Set{
		Namespace:      cfg.Namespace,
		Server:         cfg.Server,
		ServerProvider: opts.ProviderToken,
		Tailscale:      opts.TailscaleToken,
		Cloudflare:     opts.CloudflareToken,
	}
	if err := runImportWrite(opts, importWriteCredentials, func() error { return credentials.Save(cfg, creds) }); err != nil {
		return err
	}
	if err := runImportWrite(opts, importWriteState, func() error { return state.Save(stPath, st) }); err != nil {
		return err
	}
	if err := runImportWrite(opts, importWriteRegistry, func() error {
		return state.UpdateRegistry(defaultRegistryPath(), func(reg *state.Registry) error {
			cfgAbs := config.Expand(cfgPath)
			if abs, err := filepath.Abs(cfgAbs); err == nil {
				cfgAbs = abs
			}
			reg.Upsert(state.RegistryEntry{
				Namespace:       cfg.Namespace,
				Server:          cfg.Server,
				StatePath:       stPath,
				ConfigPath:      cfgAbs,
				CredentialsPath: cfg.Credentials.JSONPath,
				ResourceNames: state.RegistryResourceNames{
					ComputeServer:    cfg.Compute.Name,
					CloudflareTunnel: cfg.Cloudflare.Tunnel.Name,
				},
				Labels: cfg.Compute.Labels,
			})
			return nil
		})
	}); err != nil {
		return err
	}
	if err := privatefile.RemoveDurably(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("complete import transaction: %w", err)
	}
	return nil
}

func runImportWrite(opts ImportOptions, stage importWriteStage, write func() error) error {
	if opts.beforeWrite != nil {
		if err := opts.beforeWrite(stage); err != nil {
			return fmt.Errorf("import %s write: %w", stage, err)
		}
	}
	if err := write(); err != nil {
		return fmt.Errorf("import %s write: %w", stage, err)
	}
	return nil
}
