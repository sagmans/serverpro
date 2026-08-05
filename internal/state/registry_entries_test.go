package state

import (
	"testing"
	"time"
)

func TestUpsertFindListAndRemoveRegistryEntry(t *testing.T) {
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "web", StatePath: "/state/prod/web.json", ConfigPath: "/cfg/prod.yaml", CredentialsPath: "/creds/prod.json"})
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "api", StatePath: "/state/prod/api.json"})
	reg.Upsert(RegistryEntry{Namespace: "other", Server: "web", StatePath: "/state/other/web.json"})

	entry, ok := reg.Find("prod", "web")
	if !ok {
		t.Fatalf("missing prod/web: %+v", reg)
	}
	if entry.StatePath != "/state/prod/web.json" || entry.Namespace != "prod" || entry.Server != "web" {
		t.Fatalf("bad entry: %+v", entry)
	}
	prod := reg.List("prod")
	if len(prod) != 2 || prod[0].Server != "api" || prod[1].Server != "web" {
		t.Fatalf("prod entries not sorted: %+v", prod)
	}

	reg.Remove("prod", "web")
	if _, ok := reg.Find("prod", "web"); ok {
		t.Fatal("prod/web still present")
	}
	if got := len(reg.List("")); got != 2 {
		t.Fatalf("entries after remove = %d", got)
	}
}

func TestUpsertPreservesCreatedAtForExplicitDefaultServer(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "default", StatePath: "/state/prod/default.json", CreatedAt: created})
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "default", StatePath: "/state/prod/new-default.json"})

	entry, ok := reg.Find("prod", "")
	if !ok {
		t.Fatalf("missing default server entry: %+v", reg)
	}
	if entry.Server != "default" {
		t.Fatalf("server = %q", entry.Server)
	}
	if !entry.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %s, want %s", entry.CreatedAt, created)
	}
	if entry.StatePath != "/state/prod/new-default.json" {
		t.Fatalf("state_path = %q", entry.StatePath)
	}
}

func TestRemoveNamespaceDeletesAllNamespaceEntries(t *testing.T) {
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "web", StatePath: "/state/prod/web.json"})
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "api", StatePath: "/state/prod/api.json"})
	reg.Upsert(RegistryEntry{Namespace: "other", Server: "web", StatePath: "/state/other/web.json"})

	reg.RemoveNamespace("prod")

	if got := reg.List("prod"); len(got) != 0 {
		t.Fatalf("prod entries still present: %+v", got)
	}
	if got := reg.List(""); len(got) != 1 || got[0].Namespace != "other" {
		t.Fatalf("remaining entries = %+v", got)
	}
}
