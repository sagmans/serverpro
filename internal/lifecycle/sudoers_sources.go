package lifecycle

import "github.com/sagmans/serverpro/internal/shell"

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
	// sudoNoexecPath, sudoInterceptPath, and sudoSeshPath are the helpers
	// Ubuntu's sudo package ships and sudo uses by default: the noexec and
	// intercept libraries load into each command sudo runs as root, and
	// sesh runs as root for sudoedit. A sudo.conf Path line that points one
	// elsewhere hands root to whoever can write that file.
	sudoNoexecPath    = sudoPluginDir + "/sudo_noexec.so"
	sudoInterceptPath = sudoPluginDir + "/sudo_intercept.so"
	sudoSeshPath      = sudoPluginDir + "/sesh"
	// sudoPackagedPaths lists, as name=path words, each sudo.conf Path entry
	// that may be set and the only value it may hold. Any Path name not here
	// is reported, so a name sudo learns later fails closed.
	sudoPackagedPaths = "plugin_dir=" + sudoPluginDir + " noexec=" + sudoNoexecPath +
		" intercept=" + sudoInterceptPath + " sesh=" + sudoSeshPath
	// sudoCallerPathName is the one Path entry that may hold any value:
	// sudo runs the askpass program as the invoking user to read a
	// password, so it never runs as root.
	sudoCallerPathName = "askpass"
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
// nsswitch.conf can add SSSD or LDAP as a sudoers source. sudo.conf Path
// lines can also swap a library or helper that sudo loads or runs as root,
// so each one but askpass must hold its packaged value. Doctor and the image
// account removal both trust the parse only when neither file does any of
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
      -v nss_key=` + nsswitchSudoersKey + ` -v nss_files=` + nsswitchFilesSource + ` \
      -v packaged=` + shell.Quote(sudoPackagedPaths) + ` -v caller_path=` + sudoCallerPathName + ` '
      BEGIN {
        n = split(packaged, pairs, " ")
        for (i = 1; i <= n; i++) {
          cut = index(pairs[i], "=")
          path[substr(pairs[i], 1, cut - 1)] = substr(pairs[i], cut + 1)
        }
      }
      function gap(at, why) { printf "%s%s line %d %s\n", prefix, file, at, why }
      # conf grades one sudo.conf setting: only the sudoers.so policy from
      # the packaged directory, with no option but the safe one, passes, and
      # a Path line passes only for askpass or at its packaged value.
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
        } else if (tolower(f[1]) == "path") {
          name = tolower(f[2])
          if (name == caller_path) return
          if (!(name in path)) {
            label = (name ~ /^[a-z_]+$/) ? "Path " name : "a Path entry"
            gap(at, "sets " label ", which serverpro does not check")
          } else if (n < 3 || f[3] != path[name]) {
            gap(at, "sets Path " name " to other than the packaged " path[name] ", so sudo can load or run another file as root")
          }
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
