package cloudinit

import "strings"

// UnattendedUpgradesConfigPath is the unattended-upgrades drop-in cloud-init
// writes on first boot. APT reads apt.conf.d in lexical order, so the name
// sorts after Ubuntu's 50unattended-upgrades and its values win; a separate
// file also keeps package updates of that conffile from conflicting with it.
// Doctor reads and repairs the same path.
const UnattendedUpgradesConfigPath = "/etc/apt/apt.conf.d/52serverpro-unattended-upgrades"

// UnattendedUpgradesSetting is one boolean option of the serverpro drop-in.
// Doctor reads the effective value with apt-config's boolean type, so only
// boolean options belong here.
type UnattendedUpgradesSetting struct {
	Key   string
	Value bool
}

// UnattendedUpgradesSettings returns the drop-in in file order. It is a
// function, not a package variable, so no caller can change what later
// renders or checks see.
func UnattendedUpgradesSettings() []UnattendedUpgradesSetting {
	return []UnattendedUpgradesSetting{
		// Every kernel update installs a new image next to the old ones. The
		// noble package removes unused ones only through an in-code default
		// that its shipped file leaves commented out; stating it keeps /boot
		// from filling until a later kernel update fails to install.
		{Key: "Unattended-Upgrade::Remove-Unused-Kernel-Packages", Value: true},
	}
}

// UnattendedUpgradesConfig renders the drop-in body that cloud-init writes
// and doctor --fix restores, so both always write the same bytes.
func UnattendedUpgradesConfig() string {
	var body strings.Builder
	for _, setting := range UnattendedUpgradesSettings() {
		body.WriteString(setting.Key + " \"" + AptBool(setting.Value) + "\";\n")
	}
	return body.String()
}

// AptBool spells a boolean the way apt-config prints a /b lookup, so the
// file and doctor's comparison of the effective value use one spelling.
func AptBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// unattendedUpgradesWriteFileContent indents the drop-in for the write_files
// `content: |` block scalar.
func unattendedUpgradesWriteFileContent() string {
	var body strings.Builder
	for _, line := range strings.SplitAfter(UnattendedUpgradesConfig(), "\n") {
		if line != "" {
			body.WriteString(writeFileContentIndent + line)
		}
	}
	return body.String()
}
