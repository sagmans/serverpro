package state

import (
	"sort"
	"time"

	"github.com/sagmans/serverpro/internal/servername"
)

func (r *Registry) UpsertNamespace(namespace string) {
	r.normalize()
	project := r.Projects[namespace]
	if project.Servers == nil {
		project.Servers = map[string]RegistryEntry{}
	}
	r.Projects[namespace] = project
}

func (r *Registry) Upsert(e RegistryEntry) {
	r.normalize()
	now := time.Now().UTC()
	project := r.Projects[e.Project]
	if project.Servers == nil {
		project.Servers = map[string]RegistryEntry{}
	}
	if existing, ok := project.Servers[e.Server]; ok {
		if e.CreatedAt.IsZero() {
			e.CreatedAt = existing.CreatedAt
		}
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	project.Servers[e.Server] = e
	r.Projects[e.Project] = project
}

func (r Registry) Find(project, server string) (RegistryEntry, bool) {
	server = servername.Normalize(server)
	p, ok := r.Projects[project]
	if !ok {
		return RegistryEntry{}, false
	}
	e, ok := p.Servers[server]
	return e, ok
}

func (r Registry) ListNamespaces() []string {
	namespaces := make([]string, 0, len(r.Projects))
	for namespace := range r.Projects {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	return namespaces
}

func (r Registry) List(project string) []RegistryEntry {
	var out []RegistryEntry
	if project != "" {
		if p, ok := r.Projects[project]; ok {
			for _, e := range p.Servers {
				out = append(out, e)
			}
		}
	} else {
		for _, p := range r.Projects {
			for _, e := range p.Servers {
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Server < out[j].Server
	})
	return out
}

func (r *Registry) Remove(project, server string) {
	server = servername.Normalize(server)
	p, ok := r.Projects[project]
	if !ok {
		return
	}
	delete(p.Servers, server)
	r.Projects[project] = p
}

func (r *Registry) RemoveProject(project string) {
	delete(r.Projects, project)
}

func (r *Registry) normalize() {
	if r.SchemaVersion == 0 {
		r.SchemaVersion = RegistrySchemaVersion
	}
	if r.Projects == nil {
		r.Projects = map[string]RegistryProject{}
	}
}
