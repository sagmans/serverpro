package tailscale

type AuthKey struct {
	ID           string              `json:"id"`
	Key          string              `json:"key"`
	Description  string              `json:"description"`
	Capabilities AuthKeyCapabilities `json:"capabilities"`
}

type AuthKeyCapabilities struct {
	Devices AuthKeyDeviceCapabilities `json:"devices"`
}

type AuthKeyDeviceCapabilities struct {
	Create AuthKeyCreateCapabilities `json:"create"`
}

type AuthKeyCreateCapabilities struct {
	Tags []string `json:"tags"`
}

type Device struct {
	ID                 string   `json:"id"`
	NodeID             string   `json:"nodeId"`
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	Addresses          []string `json:"addresses"`
	Tags               []string `json:"tags"`
	Online             bool     `json:"online"`
	ConnectedToControl bool     `json:"connectedToControl"`
}

type Policy struct {
	SSH []SSHRule `json:"ssh"`
}

type SSHRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
	Users  []string `json:"users"`
}
