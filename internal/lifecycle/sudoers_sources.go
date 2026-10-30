package lifecycle

const (
	// sudoConfPath and nsswitchConfPath are the two files that decide where
	// sudo takes its rules from before it reads any sudoers file: sudo.conf
	// picks the policy plugin and passes it options, and nsswitch.conf picks
	// the sudoers sources. Ubuntu's sudo is built with these paths and with
	// SSSD support, so a "sudoers:" source there is live.
	sudoConfPath     = "/etc/sudo.conf"
	nsswitchConfPath = "/etc/nsswitch.conf"
	// sudoersPlugin and sudoPluginDir name the packaged policy plugin. A
	// Plugin line may give the bare name, which sudo resolves in the plugin
	// directory, or the full path; any other file is a different policy.
	sudoersPlugin = "sudoers.so"
	sudoPluginDir = "/usr/libexec/sudo"
	// sudoersSafePluginOption is the one sudoers.so option that changes
	// neither which rules sudo reads nor how it trusts their files: it only
	// decides whether a syntax error is fatal. Every other option (the
	// sudoers file, its owner, group, and mode, ignore_perms, LDAP settings,
	// and any unknown one) is reported, so the list fails closed.
	sudoersSafePluginOption = "error_recovery"
	// nsswitchSudoersKey starts the one nsswitch.conf line sudo reads, and
	// nsswitchFilesSource is the only source the sudoers parser covers.
	nsswitchSudoersKey  = "sudoers:"
	nsswitchFilesSource = "files"
)

// SudoersSourceGapsFunction defines the shell function sudoers_source_gaps.
// cvtsudoers reads /etc/sudoers and its includes, but sudo itself may read
// other rules: sudo.conf can load another policy plugin, point sudoers.so at
// another file, owner, or group, or move the plugin directory, and
// nsswitch.conf can add SSSD or LDAP as a sudoers source. Doctor and the
// image account removal both trust the parse only when neither file does
// that, so they share this one reader.
//
// sudoers_source_gaps prints one line per setting that does, each starting
// with $1, and returns 1 when a file exists but cannot be read. A missing
// file passes, because sudo then uses its defaults. The awk joins lines the
// way sudo_parseln does for both files: a "#" anywhere starts a comment, a
// trailing backslash continues the line only when there is no comment, and
// blanks are trimmed. Keywords match without case as sudo matches them.
// Each line number is the first physical line of the setting, so an operator
// can find it without the tool printing the file.
func SudoersSourceGapsFunction() string {
	return `sudoers_source_gaps() {
  for conf in sudo:` + sudoConfPath + ` nss:` + nsswitchConfPath + `; do
    [ -e "${conf#*:}" ] || continue
    awk -v prefix="$1" -v mode="${conf%%:*}" -v file="${conf#*:}" \
      -v plugin=` + sudoersPlugin + ` -v plugin_dir=` + sudoPluginDir + ` -v safe_option=` + sudoersSafePluginOption + ` \
      -v nss_key=` + nsswitchSudoersKey + ` -v nss_files=` + nsswitchFilesSource + ` '
      function gap(at, why) { printf "%s%s line %d %s\n", prefix, file, at, why }
      # conf grades one sudo.conf setting: only the sudoers.so policy from
      # the packaged directory, with no option but the safe one, passes.
      function conf(text, at,    n, f, i, name, label) {
        n = split(text, f, /[ \t]+/)
        if (n < 2) return
        if (tolower(f[1]) == "plugin") {
          if (n < 3) { gap(at, "has a Plugin line that names no plugin file"); return }
          if (f[3] != plugin && f[3] != plugin_dir "/" plugin) {
            gap(at, "loads a sudo plugin other than " plugin ", which can replace the sudoers policy")
            return
          }
          for (i = 4; i <= n; i++) {
            name = f[i]
            sub(/=.*/, "", name)
            if (name == safe_option) continue
            label = (name ~ /^[a-z_]+$/) ? "the " name " option" : "an option serverpro does not know"
            gap(at, "passes " plugin " " label ", which can change which sudoers rules sudo reads or how it trusts them")
          }
        } else if (tolower(f[1]) == "path" && tolower(f[2]) == "plugin_dir" && (n < 3 || f[3] != plugin_dir)) {
          gap(at, "moves the sudo plugin directory, so sudo can load a " plugin " other than the packaged one")
        }
      }
      # nss grades the first sudoers line, the only one sudo reads; the
      # bracketed actions only tune the source before them.
      function nss(text, at,    n, f, i, source) {
        if (seen || tolower(substr(text, 1, length(nss_key))) != nss_key) return
        seen = 1
        n = split(substr(text, length(nss_key) + 1), f, /[ \t]+/)
        for (i = 1; i <= n; i++) {
          source = tolower(f[i])
          if (source == "" || source == nss_files || source ~ /^\[.*\]$/) continue
          gap(at, "has sudo read sudoers rules from a source other than " nss_files)
          return
        }
      }
      function check(text, at) { if (mode == "nss") nss(text, at); else conf(text, at) }
      {
        line = $0
        sub(/\r+$/, "", line)
        if (!more) { first = NR; logical = "" }
        comment = index(line, "#")
        if (comment) line = substr(line, 1, comment - 1)
        more = !comment && line ~ /(^|[^\\])\\$/
        if (more) line = substr(line, 1, length(line) - 1)
        else sub(/[ \t]+$/, "", line)
        sub(/^[ \t]+/, "", line)
        logical = logical line
        if (!more) check(logical, first)
      }
      END { if (more) check(logical, first) }
    ' "${conf#*:}" || return 1
  done
}
`
}
