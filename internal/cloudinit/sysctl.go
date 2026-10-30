package cloudinit

import "strings"

// SysctlConfigPath is the kernel hardening drop-in cloud-init writes on first
// boot. Doctor compares the host's runtime values with this file, so the
// writer and the drift check always name the same path.
const SysctlConfigPath = "/etc/sysctl.d/99-serverpro.conf"

// writeFileContentIndent keeps each rendered line inside the write_files
// `content: |` block scalar; a shallower line would end the scalar.
const writeFileContentIndent = "      "

// SysctlSetting is one key=value line of the serverpro sysctl drop-in.
type SysctlSetting struct {
	Key   string
	Value string
}

// SysctlSettings returns the drop-in in file order. It is a function, not a
// package variable, so no caller can change what later renders or checks see.
func SysctlSettings() []SysctlSetting {
	return []SysctlSetting{
		// Strict reverse-path filtering drops packets with spoofed sources.
		{Key: "net.ipv4.conf.all.rp_filter", Value: "1"},
		{Key: "net.ipv4.conf.default.rp_filter", Value: "1"},
		// SYN cookies keep the host answering during a SYN flood.
		{Key: "net.ipv4.tcp_syncookies", Value: "1"},
		// The provider assigns addressing; router advertisements on the shared
		// segment must not add IPv6 routes behind its back.
		{Key: "net.ipv6.conf.all.accept_ra", Value: "0"},
	}
}

// sysctlWriteFileContent renders SysctlSettings as the write_files body, so
// the cloud-init file and doctor's expectations come from one list.
func sysctlWriteFileContent() string {
	var body strings.Builder
	for _, setting := range SysctlSettings() {
		body.WriteString(writeFileContentIndent + setting.Key + "=" + setting.Value + "\n")
	}
	return body.String()
}
