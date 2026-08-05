package vultr

func filterInstancesByLabel(instances []Instance, label string) []Instance {
	var out []Instance
	for _, inst := range instances {
		if inst.Label == label {
			out = append(out, inst)
		}
	}
	return out
}
