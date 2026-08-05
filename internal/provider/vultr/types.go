package vultr

import (
	"encoding/json"
	"strconv"
	"strings"
)

type Instance struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Hostname        string   `json:"hostname"`
	Region          string   `json:"region"`
	Plan            string   `json:"plan"`
	OSID            int64    `json:"os_id"`
	Status          string   `json:"status"`
	PowerStatus     string   `json:"power_status"`
	MainIP          string   `json:"main_ip"`
	V6MainIP        string   `json:"v6_main_ip"`
	Tags            []string `json:"tags"`
	FirewallGroupID string   `json:"firewall_group_id"`
}

type FirewallGroup struct {
	ID            string `json:"id"`
	Description   string `json:"description"`
	DateCreated   string `json:"date_created"`
	DateModified  string `json:"date_modified"`
	InstanceCount int    `json:"instance_count"`
	RuleCount     int    `json:"rule_count"`
	MaxRuleCount  int    `json:"max_rule_count"`
}

type FirewallRule struct {
	ID         int    `json:"id"`
	Action     string `json:"action"`
	IPType     string `json:"ip_type"`
	Protocol   string `json:"protocol"`
	Port       string `json:"port"`
	Subnet     string `json:"subnet"`
	SubnetSize int    `json:"subnet_size"`
	Source     string `json:"source"`
	Notes      string `json:"notes"`
}

type Region struct {
	ID        string   `json:"id"`
	City      string   `json:"city"`
	Country   string   `json:"country"`
	Continent string   `json:"continent"`
	Options   []string `json:"options"`
}

type Plan struct {
	ID          string   `json:"id"`
	VCPUCount   int      `json:"vcpu_count"`
	RAM         int      `json:"ram"`
	Disk        int      `json:"disk"`
	DiskCount   int      `json:"disk_count"`
	Bandwidth   int      `json:"bandwidth"`
	MonthlyCost float64  `json:"monthly_cost"`
	Type        string   `json:"type"`
	Locations   []string `json:"locations"`
	GPUCount    int      `json:"gpu_count"`
}

func (p *Plan) UnmarshalJSON(data []byte) error {
	type planAlias Plan
	var raw struct {
		planAlias
		GPUCount flexibleInt `json:"gpu_count"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = Plan(raw.planAlias)
	p.GPUCount = int(raw.GPUCount)
	return nil
}

type flexibleInt int

func (i *flexibleInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*i = flexibleInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*i = 0
		return nil
	}
	parsed, err := strconv.Atoi(s)
	if err == nil {
		*i = flexibleInt(parsed)
		return nil
	}
	if parsed, ok := parseRatioInt(s); ok {
		// Vultr shared GPU plans can expose fractional GPU counts; serverpro only
		// needs plan IDs, so keep whole-count semantics and avoid blocking catalog reads.
		*i = flexibleInt(parsed)
		return nil
	}
	return err
}

func parseRatioInt(s string) (int, bool) {
	numerator, denominator, ok := strings.Cut(s, "/")
	if !ok || numerator == "" || denominator == "" {
		return 0, false
	}
	numeratorValue, err := strconv.Atoi(numerator)
	if err != nil || numeratorValue < 0 {
		return 0, false
	}
	denominatorValue, err := strconv.Atoi(denominator)
	if err != nil || denominatorValue <= 0 {
		return 0, false
	}
	if numeratorValue%denominatorValue == 0 {
		return numeratorValue / denominatorValue, true
	}
	return 0, true
}

type OS struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Arch   string `json:"arch"`
	Family string `json:"family"`
}

type Catalog struct {
	Regions []Region
	Plans   []Plan
	OS      []OS
}

type pageMeta struct {
	Meta struct {
		Links struct {
			Next string `json:"next"`
			Prev string `json:"prev"`
		} `json:"links"`
	} `json:"meta"`
}
