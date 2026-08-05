package config

import (
	"path/filepath"
	"testing"
)

func configTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestServerStatePathSeparatesNamespaceAndServer(t *testing.T) {
	dir := configTestHome(t)
	got := ServerStatePath("prod", "web")
	want := filepath.Join(dir, ".local", "state", "serverpro", "namespaces", "prod", "servers", "web.json")
	if got != want {
		t.Fatalf("ServerStatePath() = %q, want %q", got, want)
	}
}

func TestServerConfigPathSeparatesNamespaceAndServer(t *testing.T) {
	dir := configTestHome(t)
	got := ServerConfigPath("prod", "web")
	want := filepath.Join(dir, ".config", "serverpro", "namespaces", "prod", "servers", "web.yaml")
	if got != want {
		t.Fatalf("ServerConfigPath() = %q, want %q", got, want)
	}
}

func TestServerCredentialsPathExpandsAndScopesServer(t *testing.T) {
	dir := configTestHome(t)
	got := ServerCredentialsPath("prod", "web")
	want := filepath.Join(dir, ".config", "serverpro", "namespaces", "prod", "servers", "web", "credentials.json")
	if got != want {
		t.Fatalf("ServerCredentialsPath() = %q, want %q", got, want)
	}
}
