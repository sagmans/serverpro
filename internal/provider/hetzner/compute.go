package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/provider/providerutil"
)

type ClientFactory func(token string) Client

type ComputeProvider struct {
	newClient ClientFactory
}

func NewComputeProvider(factory ClientFactory) ComputeProvider {
	if factory == nil {
		factory = New
	}
	return ComputeProvider{newClient: factory}
}

func (p ComputeProvider) Name() compute.ProviderName { return compute.ProviderName("hetzner") }

func (p ComputeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{CreateServer: true, DeleteServer: true, PowerServer: true, Catalog: true, ListServers: true}
}

func (p ComputeProvider) Doctor(ctx context.Context, account compute.Account) compute.Diagnostics {
	if account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	if _, err := p.newClient(account.Token).Locations(ctx); err != nil {
		return p.failure(account.Token, "provider credential validation failed", err)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider credential valid"}}
}

func (p ComputeProvider) Catalog(ctx context.Context, query compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	if query.Account.Token == "" {
		return compute.Catalog{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	catalog, err := p.newClient(query.Account.Token).Catalog(ctx)
	if err != nil {
		return compute.Catalog{}, p.failure(query.Account.Token, "provider catalog failed", err)
	}
	return mapCatalog(catalog, query.Location), compute.Diagnostics{{Status: compute.Pass, Message: "provider catalog loaded"}}
}

func (p ComputeProvider) Create(ctx context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	if request.Account.Token == "" {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	client := p.newClient(request.Account.Token)
	accessPolicyID, err := ensureAccessPolicy(ctx, client, request)
	if err != nil {
		return compute.ServerRecord{}, p.failure(request.Account.Token, "provider access policy create failed", err)
	}
	srv, actionID, err := client.CreateServer(ctx, createServerInputFromRequest(request), accessPolicyID, request.BootstrapData)
	if err != nil {
		record := p.reconcileCreatedServer(ctx, client, partialServerRecordFromCreateRequest(request, accessPolicyID))
		return record, p.failure(request.Account.Token, "provider server create failed", err, bootstrapRedactionSecrets(request.BootstrapData)...)
	}
	if err := client.WaitAction(ctx, actionID); err != nil {
		record := serverRecordFromCreateRequest(request, srv, accessPolicyID)
		return record, p.failure(request.Account.Token, "provider create operation failed", err, bootstrapRedactionSecrets(request.BootstrapData)...)
	}
	return serverRecordFromCreateRequest(request, srv, accessPolicyID), compute.Diagnostics{{Status: compute.Pass, Message: "provider server created"}}
}

func (p ComputeProvider) List(ctx context.Context, query compute.ListServersQuery) ([]compute.ServerRecord, compute.Diagnostics) {
	if query.Account.Token == "" {
		return nil, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	client := p.newClient(query.Account.Token)
	servers, err := client.ListServers(ctx)
	if err != nil {
		return nil, p.failure(query.Account.Token, "provider server list failed", err)
	}
	out := make([]compute.ServerRecord, 0, len(servers))
	managed := false
	for _, srv := range servers {
		record := serverRecordFromLive(srv)
		if _, _, ok := ownership.OwnershipFromLabels(record.Labels); ok {
			managed = true
		}
		out = append(out, record)
	}
	if !managed {
		return out, compute.Diagnostics{{Status: compute.Pass, Message: "provider server list loaded"}}
	}
	firewalls, err := client.ListFirewalls(ctx)
	if err != nil {
		return nil, p.failure(query.Account.Token, "provider access policy list failed", err)
	}
	for index := range out {
		if _, _, ok := ownership.OwnershipFromLabels(out[index].Labels); !ok {
			continue
		}
		policyID, err := recoverAccessPolicyID(out[index], firewalls)
		if err != nil {
			return nil, p.failure(query.Account.Token, "provider access policy recovery failed", err)
		}
		out[index].ManagedResources = []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: policyID}}
	}
	return out, compute.Diagnostics{{Status: compute.Pass, Message: "provider server list loaded"}}
}

func (p ComputeProvider) Status(ctx context.Context, ref compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	if ref.Account.Token == "" {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	id, err := serverID(ref.Record)
	if err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	srv, err := p.newClient(ref.Account.Token).GetServer(ctx, id)
	if err != nil {
		return compute.ServerStatus{}, p.failure(ref.Account.Token, "provider server status failed", err)
	}
	return statusFromServer(ref.Record, srv), compute.Diagnostics{{Status: compute.Pass, Message: "provider server status loaded"}}
}

func (p ComputeProvider) Power(ctx context.Context, request compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	if request.Account.Token == "" {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	id, err := serverID(request.Record)
	if err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := validateMutationRequest(request.Record, p.Name()); err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	current, err := client.GetServer(ctx, id)
	if err != nil {
		return compute.ServerStatus{}, p.failure(request.Account.Token, "provider ownership check failed", err)
	}
	if err := validateLiveServerOwnership(request.Record, current); err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	var actionID int64
	switch request.Action {
	case compute.PowerStart:
		actionID, err = client.PowerOnServer(ctx, id)
	case compute.PowerStop:
		actionID, err = client.ShutdownServer(ctx, id)
	case compute.PowerRestart:
		actionID, err = client.RebootServer(ctx, id)
	default:
		err = fmt.Errorf("unsupported power action %q", request.Action)
	}
	if err != nil {
		return compute.ServerStatus{}, p.failure(request.Account.Token, "provider power action failed", err)
	}
	if err := client.WaitAction(ctx, actionID); err != nil {
		return compute.ServerStatus{}, p.failure(request.Account.Token, "provider power operation failed", err)
	}
	return p.Status(ctx, compute.ServerRef{Account: request.Account, Record: request.Record})
}

func (p ComputeProvider) Delete(ctx context.Context, request compute.DeleteServerRequest) compute.Diagnostics {
	if request.Account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	accessPolicyID, err := accessPolicyID(request.Record)
	if err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider access policy id invalid"}}
	}
	if err := validateMutationRequest(request.Record, p.Name()); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	id, err := serverID(request.Record)
	if err != nil {
		if accessPolicyID == 0 {
			return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
		}
		return p.deleteAccessPolicy(ctx, client, request.Account.Token, request.Record, accessPolicyID)
	}
	current, err := client.GetServer(ctx, id)
	serverAlreadyDeleted := httpjson.IsStatus(err, http.StatusNotFound)
	if err != nil && !serverAlreadyDeleted {
		return p.failure(request.Account.Token, "provider ownership check failed", err)
	}
	if !serverAlreadyDeleted {
		if err := validateLiveServerOwnership(request.Record, current); err != nil {
			return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
		}
		actionID, err := client.DeleteServer(ctx, id)
		if err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return p.failure(request.Account.Token, "provider server delete failed", err)
		}
		if err == nil {
			if err := client.WaitAction(ctx, actionID); err != nil {
				return p.failure(request.Account.Token, "provider delete operation failed", err)
			}
		}
	}
	if accessPolicyID != 0 {
		return p.deleteAccessPolicy(ctx, client, request.Account.Token, request.Record, accessPolicyID)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider server deleted"}}
}

func (p ComputeProvider) deleteAccessPolicy(ctx context.Context, client Client, secret string, record compute.ServerRecord, id int64) compute.Diagnostics {
	firewall, err := client.GetFirewall(ctx, id)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return compute.Diagnostics{{Status: compute.Pass, Message: "provider server deleted"}}
	}
	if err != nil {
		return p.failure(secret, "provider access policy ownership check failed", err)
	}
	if err := validateLiveAccessPolicyOwnership(record, firewall); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := client.DeleteFirewall(ctx, id); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
		return p.failure(secret, "provider access policy delete failed", err)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider server deleted"}}
}

func serverID(record compute.ServerRecord) (int64, error) {
	id, err := strconv.ParseInt(record.ID, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("provider server id missing")
	}
	return id, nil
}

func statusFromServer(record compute.ServerRecord, srv Server) compute.ServerStatus {
	record.Name = srv.Name
	record.PublicIPv4 = srv.PublicNet.IPv4.IP
	record.PublicIPv6 = srv.PublicNet.IPv6.IP
	if len(srv.Labels) > 0 {
		record.Labels = srv.Labels
	}
	return compute.ServerStatus{Record: record, Power: srv.Status, PublicIPv4: srv.PublicNet.IPv4.IP, PublicIPv6: srv.PublicNet.IPv6.IP}
}

func serverRecordFromLive(srv Server) compute.ServerRecord {
	namespace, server, _ := ownership.OwnershipFromLabels(srv.Labels)
	location := srv.Location.Name
	if location == "" {
		location = srv.Datacenter.Location.Name
	}
	image := srv.Image.Name
	return compute.ServerRecord{
		Provider:   compute.ProviderName("hetzner"),
		Namespace:  namespace,
		Server:     server,
		ID:         strconv.FormatInt(srv.ID, 10),
		Name:       srv.Name,
		Location:   location,
		Size:       srv.ServerType.Name,
		Image:      image,
		PublicIPv4: srv.PublicNet.IPv4.IP,
		PublicIPv6: srv.PublicNet.IPv6.IP,
		Labels:     srv.Labels,
	}
}

func recoverAccessPolicyID(record compute.ServerRecord, firewalls []Firewall) (string, error) {
	expectedName := record.Name + "-deny-public"
	var matches []Firewall
	for _, firewall := range firewalls {
		if firewall.Name == expectedName && ownership.LiveLabelsMatch(firewall.Labels, record.Namespace, record.Server) {
			matches = append(matches, firewall)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("provider access policy %q not found", expectedName)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("provider access policy %q is ambiguous", expectedName)
	}
	if matches[0].ID == 0 {
		return "", fmt.Errorf("provider access policy %q id missing", expectedName)
	}
	return strconv.FormatInt(matches[0].ID, 10), nil
}

func accessPolicyID(record compute.ServerRecord) (int64, error) {
	raw, _ := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if raw == "" {
		raw = record.ProviderState["access_policy_id"]
	}
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func validateMutationRequest(record compute.ServerRecord, provider compute.ProviderName) error {
	return providerutil.ValidateMutationProvider(record.Provider, provider)
}

func validateLiveAccessPolicyOwnership(record compute.ServerRecord, firewall Firewall) error {
	if record.Name == "" {
		return fmt.Errorf("provider ownership mismatch: state server name missing")
	}
	expectedName := record.Name + "-deny-public"
	if firewall.Name != expectedName {
		return fmt.Errorf("provider ownership mismatch: live access policy name %q state %q", firewall.Name, expectedName)
	}
	return ownership.ValidateLiveLabels(firewall.Labels, record.Namespace, record.Server)
}

func validateLiveServerOwnership(record compute.ServerRecord, srv Server) error {
	if record.Name != "" && srv.Name != "" && srv.Name != record.Name {
		return fmt.Errorf("provider ownership mismatch: live server name %q state server name %q", srv.Name, record.Name)
	}
	return ownership.ValidateLiveLabels(srv.Labels, record.Namespace, record.Server)
}

func (p ComputeProvider) failure(secret, prefix string, err error, extraSecrets ...string) compute.Diagnostics {
	return providerutil.Failure(secret, prefix, err, extraSecrets...)
}

func bootstrapRedactionSecrets(data string) []string {
	return providerutil.BootstrapSecrets(data)
}

func ensureAccessPolicy(ctx context.Context, client Client, request compute.CreateServerRequest) (int64, error) {
	if raw, ok := compute.ManagedResourceID(request.ManagedResources, compute.ManagedResourceAccessPolicy); ok {
		return validateCheckpointAccessPolicy(ctx, client, request, raw)
	}
	if raw := request.ProviderState["access_policy_id"]; raw != "" {
		return validateCheckpointAccessPolicy(ctx, client, request, raw)
	}
	firewall, err := client.CreateFirewall(ctx, request.Intent.Name+"-deny-public", providerLabelsFromRequest(request))
	if err != nil {
		return 0, err
	}
	return firewall.ID, nil
}

func validateCheckpointAccessPolicy(ctx context.Context, client Client, request compute.CreateServerRequest, rawID string) (int64, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("provider access policy id invalid")
	}
	firewall, err := client.GetFirewall(ctx, id)
	if err != nil {
		return 0, err
	}
	record := partialServerRecordFromCreateRequest(request, id)
	if firewall.ID != id {
		return 0, fmt.Errorf("provider ownership mismatch: live access policy id %d state %d", firewall.ID, id)
	}
	if err := validateLiveAccessPolicyOwnership(record, firewall); err != nil {
		return 0, err
	}
	if len(firewall.Rules) != 0 {
		return 0, fmt.Errorf("provider access policy has unexpected rules")
	}
	if len(firewall.AppliedTo) != 0 {
		return 0, fmt.Errorf("provider ownership mismatch: live access policy has attachment")
	}
	return id, nil
}

func createServerInputFromRequest(request compute.CreateServerRequest) CreateServerInput {
	return CreateServerInput{Name: request.Intent.Name, Location: request.Intent.Location, Size: request.Intent.Size, Image: request.Intent.Image, Labels: providerLabelsFromRequest(request)}
}

func providerLabelsFromRequest(request compute.CreateServerRequest) map[string]string {
	return ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels)
}

func serverRecordFromCreateRequest(request compute.CreateServerRequest, srv Server, accessPolicyID int64) compute.ServerRecord {
	record := partialServerRecordFromCreateRequest(request, accessPolicyID)
	record.ID = strconv.FormatInt(srv.ID, 10)
	record.Name = srv.Name
	record.PublicIPv4 = srv.PublicNet.IPv4.IP
	record.PublicIPv6 = srv.PublicNet.IPv6.IP
	return record
}

func (p ComputeProvider) reconcileCreatedServer(ctx context.Context, client Client, record compute.ServerRecord) compute.ServerRecord {
	srv, err := client.FindServerByName(ctx, record.Name)
	if err != nil {
		return record
	}
	if err := validateLiveServerOwnership(record, srv); err != nil {
		return record
	}
	record.ID = strconv.FormatInt(srv.ID, 10)
	record.Name = srv.Name
	record.PublicIPv4 = srv.PublicNet.IPv4.IP
	record.PublicIPv6 = srv.PublicNet.IPv6.IP
	if len(srv.Labels) > 0 {
		record.Labels = srv.Labels
	}
	return record
}

func partialServerRecordFromCreateRequest(request compute.CreateServerRequest, accessPolicyID int64) compute.ServerRecord {
	return compute.ServerRecord{
		Provider:         compute.ProviderName("hetzner"),
		Namespace:        request.Intent.Namespace,
		Server:           request.Intent.Server,
		Name:             request.Intent.Name,
		Location:         request.Intent.Location,
		Size:             request.Intent.Size,
		Image:            request.Intent.Image,
		Labels:           providerLabelsFromRequest(request),
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: strconv.FormatInt(accessPolicyID, 10)}},
	}
}

func mapCatalog(catalog Catalog, location string) compute.Catalog {
	out := compute.Catalog{
		Locations: make([]compute.Location, 0, len(catalog.Locations)),
		Sizes:     make([]compute.Size, 0, len(catalog.ServerTypes)),
		Images:    make([]compute.Image, 0, len(catalog.Images)),
	}
	for _, loc := range catalog.Locations {
		out.Locations = append(out.Locations, compute.Location{Name: loc.Name, Description: loc.Description, Country: loc.Country, City: loc.City, Zone: loc.NetworkZone})
	}
	for _, serverType := range catalog.ServerTypes {
		if serverType.Deprecated || serverType.Deprecation != nil || (location != "" && !serverType.SupportsLocation(location)) {
			continue
		}
		out.Sizes = append(out.Sizes, compute.Size{Name: serverType.Name, Description: serverType.Description, Cores: serverType.Cores, MemoryGB: serverType.Memory, DiskGB: serverType.Disk, Architecture: serverType.Architecture, Locations: serverTypeLocationNames(serverType)})
	}
	for _, image := range catalog.Images {
		if image.Status != "" && image.Status != "available" || image.Deprecated != nil {
			continue
		}
		out.Images = append(out.Images, compute.Image{Name: image.Name, Description: image.Description, Architecture: image.Architecture, OSFlavor: image.OSFlavor, OSVersion: image.OSVersion})
	}
	return out
}

func serverTypeLocationNames(serverType ServerType) []string {
	locations := make([]string, 0, len(serverType.Locations))
	for _, location := range serverType.Locations {
		if location.Deprecated || location.Deprecation != nil {
			continue
		}
		locations = append(locations, location.Name)
	}
	return locations
}
