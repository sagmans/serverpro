package cli

import (
	"context"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func lockServerArtifactWorkflow(ctx context.Context, st state.State) (func(), error) {
	return state.LockNamespaceOperation(ctx, config.RegistryPath(), st.NamespaceName())
}
