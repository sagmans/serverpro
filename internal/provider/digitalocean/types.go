package digitalocean

type Region struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Features  []string `json:"features"`
	Sizes     []string `json:"sizes"`
}

type Size struct {
	Slug         string   `json:"slug"`
	Memory       int      `json:"memory"`
	VCPUs        int      `json:"vcpus"`
	Disk         int      `json:"disk"`
	PriceMonthly float64  `json:"price_monthly"`
	Description  string   `json:"description"`
	Regions      []string `json:"regions"`
	Available    bool     `json:"available"`
}

type Image struct {
	ID           int64    `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Distribution string   `json:"distribution"`
	Regions      []string `json:"regions"`
	Status       string   `json:"status"`
	Public       bool     `json:"public"`
	Type         string   `json:"type"`
}

type Droplet struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Networks Networks `json:"networks"`
	Tags     []string `json:"tags"`
}

type Networks struct {
	V4 []NetworkV4 `json:"v4"`
	V6 []NetworkV6 `json:"v6"`
}

type NetworkV4 struct {
	IPAddress string `json:"ip_address"`
	Type      string `json:"type"`
}

type NetworkV6 struct {
	IPAddress string `json:"ip_address"`
	Type      string `json:"type"`
}

type Firewall struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Tags       []string `json:"tags"`
	DropletIDs []int64  `json:"droplet_ids"`
	Status     string   `json:"status"`
	Inbound    []Rule   `json:"inbound_rules"`
	Outbound   []Rule   `json:"outbound_rules"`
}

type Rule struct {
	Protocol     string       `json:"protocol"`
	Ports        string       `json:"ports"`
	Sources      *RuleTargets `json:"sources,omitempty"`
	Destinations *RuleTargets `json:"destinations,omitempty"`
}

type RuleTargets struct {
	Addresses        []string `json:"addresses,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	DropletIDs       []int64  `json:"droplet_ids,omitempty"`
	LoadBalancerUIDs []string `json:"load_balancer_uids,omitempty"`
	KubernetesIDs    []string `json:"kubernetes_ids,omitempty"`
}

type Action struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Type   string `json:"type"`
}

type Catalog struct {
	Regions []Region
	Sizes   []Size
	Images  []Image
}

type pageMeta struct {
	Links struct {
		Pages struct {
			Next string `json:"next"`
		} `json:"pages"`
	} `json:"links"`
}
