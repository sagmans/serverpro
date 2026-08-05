package state

import (
	"sort"
	"time"

	"github.com/sagmans/serverpro/internal/servername"
)

func (r *Registry) UpsertNamespace(namespace string) {
	r.normalize()
	group := r.Namespaces[namespace]
	if group.Servers == nil {
		group.Servers = map[string]RegistryEntry{}
	}
	r.Namespaces[namespace] = group
}

func (r *Registry) Upsert(e RegistryEntry) {
	r.normalize()
	now := time.Now().UTC()
	group := r.Namespaces[e.Namespace]
	if group.Servers == nil {
		group.Servers = map[string]RegistryEntry{}
	}
	if existing, ok := group.Servers[e.Server]; ok && e.CreatedAt.IsZero() {
		e.CreatedAt = existing.CreatedAt
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	group.Servers[e.Server] = e
	r.Namespaces[e.Namespace] = group
}

func (r Registry) Find(namespace, server string) (RegistryEntry, bool) {
	server = servername.Normalize(server)
	group, ok := r.Namespaces[namespace]
	if !ok {
		return RegistryEntry{}, false
	}
	e, ok := group.Servers[server]
	return e, ok
}

func (r Registry) ListNamespaces() []string {
	namespaces := make([]string, 0, len(r.Namespaces))
	for namespace := range r.Namespaces {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	return namespaces
}

func (r Registry) List(namespace string) []RegistryEntry {
	var out []RegistryEntry
	if namespace != "" {
		if group, ok := r.Namespaces[namespace]; ok {
			for _, e := range group.Servers {
				out = append(out, e)
			}
		}
	} else {
		for _, group := range r.Namespaces {
			for _, e := range group.Servers {
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Server < out[j].Server
	})
	return out
}

func (r *Registry) Remove(namespace, server string) {
	server = servername.Normalize(server)
	group, ok := r.Namespaces[namespace]
	if !ok {
		return
	}
	delete(group.Servers, server)
	r.Namespaces[namespace] = group
}

func (r *Registry) RemoveNamespace(namespace string) {
	delete(r.Namespaces, namespace)
}

func (r *Registry) normalize() {
	if r.SchemaVersion == 0 {
		r.SchemaVersion = RegistrySchemaVersion
	}
	if r.Namespaces == nil {
		r.Namespaces = map[string]RegistryNamespace{}
	}
	for namespace, group := range r.Namespaces {
		for server, entry := range group.Servers {
			entry.Namespace = namespace
			group.Servers[server] = entry
		}
		r.Namespaces[namespace] = group
	}
}
