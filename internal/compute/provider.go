package compute

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type ProviderName string

type Provider interface {
	Name() ProviderName
	Capabilities(context.Context) Capabilities
	Doctor(context.Context, Account) Diagnostics
	Catalog(context.Context, CatalogQuery) (Catalog, Diagnostics)
	// List returns account compute inventory so local state can be rebuilt from ownership labels.
	List(context.Context, ListServersQuery) ([]ServerRecord, Diagnostics)
	Create(context.Context, CreateServerRequest) (ServerRecord, Diagnostics)
	Status(context.Context, ServerRef) (ServerStatus, Diagnostics)
	Power(context.Context, PowerRequest) (ServerStatus, Diagnostics)
	Delete(context.Context, DeleteServerRequest) Diagnostics
}

// ImageReferencePolicy lets adapters validate opaque image identifiers without
// requiring credentials or leaking provider details into generic callers.
type ImageReferencePolicy interface {
	SupportsImageReference(string) bool
}

type Account struct {
	Name     string
	Provider ProviderName
	Token    string
	Scope    string
}

type Capabilities struct {
	CreateServer bool `json:"create_server"`
	DeleteServer bool `json:"delete_server"`
	PowerServer  bool `json:"power_server"`
	Catalog      bool `json:"catalog"`
	ListServers  bool `json:"list_servers"`
}

type CatalogQuery struct {
	Account  Account
	Location string
}

// ListServersQuery scopes provider inventory reads used by discover/import recovery.
type ListServersQuery struct {
	Account Account
}

type ServerIntent struct {
	Namespace string            `json:"namespace"`
	Server    string            `json:"server"`
	Name      string            `json:"name"`
	Location  string            `json:"location"`
	Size      string            `json:"size"`
	Image     string            `json:"image"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type CreateServerRequest struct {
	Account       Account           `json:"account"`
	Intent        ServerIntent      `json:"intent"`
	BootstrapData string            `json:"-"`
	ProviderState map[string]string `json:"provider_state,omitempty"`
	// CheckpointProviderState prevents compute creation until newly created
	// provider resources have a durable lifecycle-owned recovery handle.
	CheckpointProviderState func(map[string]string) error `json:"-"`
}

type ServerRecord struct {
	Provider      ProviderName      `json:"provider"`
	Account       string            `json:"account,omitempty"`
	Namespace     string            `json:"namespace"`
	Server        string            `json:"server"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location,omitempty"`
	Size          string            `json:"size,omitempty"`
	Image         string            `json:"image,omitempty"`
	PublicIPv4    string            `json:"public_ipv4,omitempty"`
	PublicIPv6    string            `json:"public_ipv6,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	ProviderState map[string]string `json:"provider_state,omitempty"`
}

type ServerRef struct {
	Account Account      `json:"account"`
	Record  ServerRecord `json:"record"`
}

type ServerStatus struct {
	Record     ServerRecord `json:"record"`
	Power      string       `json:"power"`
	PublicIPv4 string       `json:"public_ipv4,omitempty"`
	PublicIPv6 string       `json:"public_ipv6,omitempty"`
}

type PowerAction string

const (
	PowerStart   PowerAction = "start"
	PowerStop    PowerAction = "stop"
	PowerRestart PowerAction = "restart"
)

type PowerRequest struct {
	Account Account      `json:"account"`
	Record  ServerRecord `json:"record"`
	Action  PowerAction  `json:"action"`
}

type DeleteServerRequest struct {
	Account Account      `json:"account"`
	Record  ServerRecord `json:"record"`
}

type Catalog struct {
	Locations []Location `json:"locations"`
	Sizes     []Size     `json:"sizes"`
	Images    []Image    `json:"images"`
}

type Location struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Country     string `json:"country,omitempty"`
	City        string `json:"city,omitempty"`
	Zone        string `json:"zone,omitempty"`
}

type Size struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Cores        int      `json:"cores,omitempty"`
	MemoryGB     float64  `json:"memory_gb,omitempty"`
	DiskGB       int      `json:"disk_gb,omitempty"`
	Architecture string   `json:"architecture,omitempty"`
	Locations    []string `json:"locations,omitempty"`
}

type Image struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	OSFlavor     string `json:"os_flavor,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
}

type DiagnosticStatus string

const (
	Pass DiagnosticStatus = "pass"
	Fail DiagnosticStatus = "fail"
	Warn DiagnosticStatus = "warn"
)

type Diagnostic struct {
	Status  DiagnosticStatus `json:"status"`
	Message string           `json:"message"`
}

type Diagnostics []Diagnostic

func (d Diagnostics) Passed() bool {
	for _, diagnostic := range d {
		if diagnostic.Status == Fail {
			return false
		}
	}
	return true
}

func (d Diagnostics) Err() error {
	var messages []error
	for _, diagnostic := range d {
		if diagnostic.Status == Fail {
			messages = append(messages, errors.New(diagnostic.Message))
		}
	}
	return errors.Join(messages...)
}

type Registry struct {
	providers map[ProviderName]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[ProviderName]Provider)}
}

func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("provider required")
	}
	name := provider.Name()
	if name == "" {
		return fmt.Errorf("provider name required")
	}
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = provider
	return nil
}

func (r *Registry) Get(name ProviderName) (Provider, bool) {
	provider, ok := r.providers[name]
	return provider, ok
}

func (r *Registry) List() []Provider {
	providers := make([]Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name() < providers[j].Name() })
	return providers
}
