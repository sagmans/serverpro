package hetzner

type Firewall struct {
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	Labels    map[string]string  `json:"labels"`
	Rules     []FirewallRule     `json:"rules"`
	AppliedTo []FirewallResource `json:"applied_to"`
}

type FirewallRule struct{}

type FirewallResource struct{}

type Server struct {
	ID              int64             `json:"id"`
	Name            string            `json:"name"`
	Status          string            `json:"status"`
	Labels          map[string]string `json:"labels"`
	PublicNet       PublicNet         `json:"public_net"`
	ServerType      ServerType        `json:"server_type"`
	Image           Image             `json:"image"`
	Location        Location          `json:"location"`
	Datacenter      Datacenter        `json:"datacenter"`
	IncludedTraffic int64             `json:"included_traffic"`
	IngoingTraffic  int64             `json:"ingoing_traffic"`
	OutgoingTraffic int64             `json:"outgoing_traffic"`
}

type Datacenter struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Location Location `json:"location"`
}

type PublicNet struct {
	IPv4 IPAddr `json:"ipv4"`
	IPv6 IPAddr `json:"ipv6"`
}

type IPAddr struct {
	IP string `json:"ip"`
}

type Action struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}
