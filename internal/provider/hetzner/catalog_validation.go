package hetzner

import "fmt"

func (c Catalog) ValidateSelection(locationName, serverTypeName, imageName string) error {
	var locOK bool
	for _, loc := range c.Locations {
		if loc.Name == locationName {
			locOK = true
			break
		}
	}
	if !locOK {
		return fmt.Errorf("hetzner location %q not found", locationName)
	}
	var st *ServerType
	for i := range c.ServerTypes {
		if c.ServerTypes[i].Name == serverTypeName {
			st = &c.ServerTypes[i]
			break
		}
	}
	if st == nil {
		return fmt.Errorf("hetzner server type %q not found", serverTypeName)
	}
	if st.Deprecation != nil || st.Deprecated {
		return fmt.Errorf("hetzner server type %q is deprecated", serverTypeName)
	}
	if !st.SupportsLocation(locationName) {
		return fmt.Errorf("hetzner server type %q not supported in location %q", serverTypeName, locationName)
	}
	for _, loc := range st.Locations {
		if loc.Name == locationName && (loc.Deprecation != nil || loc.Deprecated) {
			return fmt.Errorf("hetzner server type %q is deprecated in location %q", serverTypeName, locationName)
		}
	}
	foundImage := false
	foundAvailable := false
	foundNotDeprecated := false
	for i := range c.Images {
		img := &c.Images[i]
		if img.Name != imageName {
			continue
		}
		foundImage = true
		if img.Status != "" && img.Status != "available" {
			continue
		}
		foundAvailable = true
		if img.Deprecated != nil {
			continue
		}
		foundNotDeprecated = true
		if st.Architecture == "" || img.Architecture == "" || st.Architecture == img.Architecture {
			return nil
		}
	}
	if !foundImage {
		return fmt.Errorf("hetzner image %q not found", imageName)
	}
	if !foundAvailable {
		return fmt.Errorf("hetzner image %q is not available", imageName)
	}
	if !foundNotDeprecated {
		return fmt.Errorf("hetzner image %q is deprecated", imageName)
	}
	return fmt.Errorf("hetzner image %q has no architecture compatible with server type %q architecture %q", imageName, serverTypeName, st.Architecture)
}
