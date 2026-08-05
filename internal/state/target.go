package state

import "fmt"

type Target struct {
	Namespace            string
	Server               string
	ComputeServerName    string
	CloudflareTunnelName string
}

func ValidateTarget(target Target, st State) error {
	if st.NamespaceName() != target.Namespace {
		return fmt.Errorf("state target mismatch: state namespace %q config namespace %q", st.NamespaceName(), target.Namespace)
	}
	if st.Server != target.Server {
		return fmt.Errorf("state target mismatch: state server %q config server %q", st.Server, target.Server)
	}
	if st.Compute.Name != "" && st.Compute.Name != target.ComputeServerName {
		return fmt.Errorf("state target mismatch: state server name %q config server name %q", st.Compute.Name, target.ComputeServerName)
	}
	if st.Cloudflare.Name != "" && st.Cloudflare.Name != target.CloudflareTunnelName {
		return fmt.Errorf("state target mismatch: state tunnel name %q config tunnel name %q", st.Cloudflare.Name, target.CloudflareTunnelName)
	}
	return nil
}
