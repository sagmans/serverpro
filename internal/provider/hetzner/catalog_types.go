package hetzner

type Location struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Country     string  `json:"country"`
	City        string  `json:"city"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	NetworkZone string  `json:"network_zone"`
}

type ServerTypeLocation struct {
	Name        string `json:"name"`
	Deprecated  bool   `json:"deprecated"`
	Deprecation any    `json:"deprecation"`
}

type ServerType struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	Category        string               `json:"category"`
	Cores           int                  `json:"cores"`
	Memory          float64              `json:"memory"`
	Disk            int                  `json:"disk"`
	StorageType     string               `json:"storage_type"`
	CPUType         string               `json:"cpu_type"`
	Architecture    string               `json:"architecture"`
	Deprecated      bool                 `json:"deprecated"`
	Deprecation     any                  `json:"deprecation"`
	IncludedTraffic int64                `json:"included_traffic"`
	Locations       []ServerTypeLocation `json:"locations"`
}

type Image struct {
	ID           int64  `json:"id"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Architecture string `json:"architecture"`
	OSFlavor     string `json:"os_flavor"`
	OSVersion    string `json:"os_version"`
	Deprecated   any    `json:"deprecated"`
}

type Catalog struct {
	Locations   []Location
	ServerTypes []ServerType
	Images      []Image
}

type pageMeta struct {
	Meta struct {
		Pagination struct {
			NextPage *int `json:"next_page"`
		} `json:"pagination"`
	} `json:"meta"`
}

func (s ServerType) SupportsLocation(location string) bool {
	for _, loc := range s.Locations {
		if loc.Name == location {
			return true
		}
	}
	return len(s.Locations) == 0
}
