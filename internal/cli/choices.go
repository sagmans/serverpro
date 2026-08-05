package cli

func computeLocationChoices() []choice {
	return []choice{
		{"fsn1", "Falkenstein, DE · eu-central · Falkenstein DC Park"},
		{"nbg1", "Nuremberg, DE · eu-central · Nuremberg DC Park"},
		{"hel1", "Helsinki, FI · eu-central · Helsinki DC Park"},
		{"ash", "Ashburn, VA, US · us-east · Cloud location"},
		{"hil", "Hillsboro, OR, US · us-west · Cloud location"},
		{"sin", "Singapore, SG · ap-southeast · Cloud location"},
	}
}

func computeSizeChoices() []choice {
	return []choice{
		{"cax11", "ARM Ampere shared · 2 vCPU · 4 GB RAM · 40 GB disk"},
		{"cax21", "ARM Ampere shared · 4 vCPU · 8 GB RAM · 80 GB disk"},
		{"cax31", "ARM Ampere shared · 8 vCPU · 16 GB RAM · 160 GB disk"},
		{"cax41", "ARM Ampere shared · 16 vCPU · 32 GB RAM · 320 GB disk"},
		{"cx23", "cost-optimized x86 shared · 2 vCPU · 4 GB RAM · 40 GB disk"},
		{"cx33", "cost-optimized x86 shared · 4 vCPU · 8 GB RAM · 80 GB disk"},
		{"cx43", "cost-optimized x86 shared · 8 vCPU · 16 GB RAM · 160 GB disk"},
		{"cx53", "cost-optimized x86 shared · 16 vCPU · 32 GB RAM · 320 GB disk"},
		{"cpx22", "regular-performance AMD shared · 2 vCPU · 4 GB RAM · 80 GB disk"},
		{"cpx32", "regular-performance AMD shared · 4 vCPU · 8 GB RAM · 160 GB disk"},
		{"cpx42", "regular-performance AMD shared · 8 vCPU · 16 GB RAM · 320 GB disk"},
		{"cpx52", "regular-performance AMD shared · 12 vCPU · 24 GB RAM · 480 GB disk"},
		{"cpx62", "regular-performance AMD shared · 16 vCPU · 32 GB RAM · 640 GB disk"},
		{"ccx13", "dedicated x86 CPU · 2 vCPU · 8 GB RAM · 80 GB disk"},
		{"ccx23", "dedicated x86 CPU · 4 vCPU · 16 GB RAM · 160 GB disk"},
		{"ccx33", "dedicated x86 CPU · 8 vCPU · 32 GB RAM · 240 GB disk"},
	}
}

func computeImageChoices() []choice {
	return []choice{
		{"ubuntu-24.04", "Ubuntu 24.04 LTS · current safe default · x86/arm"},
		{"ubuntu-26.04", "Ubuntu 26.04 · validate availability live before create · x86/arm"},
		{"ubuntu-22.04", "Ubuntu 22.04 LTS · stable · x86/arm"},
		{"debian-12", "Debian 12 · stable · x86/arm"},
		{"fedora-44", "Fedora 44 · validate availability live before create · x86/arm"},
	}
}
