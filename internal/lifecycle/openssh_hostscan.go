package lifecycle

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The OpenSSH-off host scan reads a locally managed cloudflared's settings
// with the allowlist tunnelRouteToSSH applies to the remote ingress, so the
// two halves of the preflight agree on what reaches sshd. The host has no
// parser for cloudflared's YAML or flags, so an awk program reads them, and
// a form it cannot read whole counts as a route, never as a pass.

const (
	// cloudflaredServiceKey and cloudflaredURLFlag pick an origin: an ingress
	// rule's service, and the legacy single origin that --url, TUNNEL_URL, or
	// a url: key in the config file sets (upstream
	// ingress.UnvalidatedIngressRule and parseSingleOriginService).
	cloudflaredServiceKey = "service"
	cloudflaredURLFlag    = "url"
	cloudflaredURLEnv     = "TUNNEL_URL"
	// cloudflaredBastionModeKey, cloudflaredProxyTypeKey,
	// cloudflaredBastionFlag, and cloudflaredSOCKS5Flag let a client choose
	// the destination: originRequest bastionMode and proxyType, and the
	// --bastion and --socks5 modes, which cloudflared also reads from a config
	// file key of the same name or from their environment variables (upstream
	// OriginRequestConfig, config.BastionFlag, and the tunnel flags' EnvVars).
	cloudflaredBastionModeKey = "bastionMode"
	cloudflaredProxyTypeKey   = "proxyType"
	cloudflaredBastionFlag    = "bastion"
	cloudflaredBastionEnv     = "TUNNEL_BASTION"
	cloudflaredSOCKS5Flag     = "socks5"
	cloudflaredSOCKS5Env      = "TUNNEL_SOCKS"
	// cloudflaredHomeEnv is what cloudflared expands ~ with in its config
	// search, so a unit that sets it moves the search off the homes the
	// account database lists.
	cloudflaredHomeEnv = "HOME"
	// tunnelSSHPortRoute, tunnelRawTCPRoute, and tunnelSOCKS5Route name host
	// findings next to the shared labels in openssh.go. The host scan prints
	// a label, never a value, because the same settings hold the tunnel token.
	tunnelSSHPortRoute = "an origin on the SSH port"
	tunnelRawTCPRoute  = "an origin cloudflared reads as raw TCP"
	tunnelSOCKS5Route  = "a SOCKS5 proxy"
	// hostScanConfigMode, hostScanSettingsMode, and hostScanHomeMode select
	// what the awk program reads: a config file, unit and environment file
	// settings, or the HOME those settings set.
	hostScanConfigMode   = "config"
	hostScanSettingsMode = "settings"
	hostScanHomeMode     = "home"
	// hostScanService, hostScanProxy, hostScanBastion, hostScanSOCKS5, and
	// hostScanHome say how the program judges a setting's value.
	hostScanService = "service"
	hostScanProxy   = "proxy"
	hostScanBastion = "bastion"
	hostScanSOCKS5  = "socks5"
	hostScanHome    = "home"
)

// hostScanFalse holds strconv.ParseBool's false spellings, the only values
// that turn a bastion or SOCKS5 setting off for both the flag parser and the
// config file. Any other value, a YAML yes or no included, counts as on.
var hostScanFalse = []string{"0", "f", "F", "false", "False", "FALSE"}

// hostScanSetting is one cloudflared setting the program looks for: a config
// file key, a flag (one or two dashes, as urfave/cli accepts), or an
// environment variable, and how its value is judged.
type hostScanSetting struct {
	name  string
	env   bool
	judge string
}

// hostScanConfigKeys are matched without regard to case or quotes, which is
// stricter than cloudflared's YAML decoder.
var hostScanConfigKeys = []hostScanSetting{
	{name: cloudflaredServiceKey, judge: hostScanService},
	{name: cloudflaredURLFlag, judge: hostScanService},
	{name: cloudflaredBastionModeKey, judge: hostScanBastion},
	{name: cloudflaredProxyTypeKey, judge: hostScanProxy},
	{name: cloudflaredBastionFlag, judge: hostScanBastion},
	{name: cloudflaredSOCKS5Flag, judge: hostScanSOCKS5},
}

// hostScanUnitSettings are matched in a unit's ExecStart and Environment and
// in the environment files it reads.
var hostScanUnitSettings = []hostScanSetting{
	{name: cloudflaredURLFlag, judge: hostScanService},
	{name: cloudflaredURLEnv, env: true, judge: hostScanService},
	{name: cloudflaredBastionFlag, judge: hostScanBastion},
	{name: cloudflaredBastionEnv, env: true, judge: hostScanBastion},
	{name: cloudflaredSOCKS5Flag, judge: hostScanSOCKS5},
	{name: cloudflaredSOCKS5Env, env: true, judge: hostScanSOCKS5},
	{name: cloudflaredHomeEnv, env: true, judge: hostScanHome},
}

// hostScanProgram is the awk program the host scan runs with -v mode=. In
// the config and settings modes it prints the label of the first setting
// that may reach sshd and exits 0, or prints nothing; the home mode prints
// each HOME the settings set. A read error is awk's own non-zero exit, so
// the script tells an unreadable file from a clean one.
func hostScanProgram() string {
	var b strings.Builder
	b.WriteString("BEGIN {\n")
	assign := func(name, value string) {
		fmt.Fprintf(&b, "\t%s = %s\n", name, awkString(value))
	}
	assign("config_mode", hostScanConfigMode)
	assign("home_mode", hostScanHomeMode)
	fmt.Fprintf(&b, "\tport = %d\n", OpenSSHPort)
	fmt.Fprintf(&b, "\tmax_port = %d\n", tunnelMaxPort)
	assign("tcp", tunnelTCPScheme)
	schemes := make([]string, 0, len(tunnelWebPorts))
	for scheme := range tunnelWebPorts {
		schemes = append(schemes, scheme)
	}
	sort.Strings(schemes)
	for _, scheme := range schemes {
		fmt.Fprintf(&b, "\tweb[%s] = %s\n", awkString(scheme), awkString(tunnelWebPorts[scheme]))
	}
	assign("unix_prefix", tunnelUnixPrefix)
	assign("unix_tls_prefix", tunnelUnixTLSPrefix)
	assign("status_prefix", tunnelStatusServicePrefix)
	assign("hello", tunnelHelloWorldService)
	assign("hello_flag", tunnelHelloWorldFlag)
	assign("bastion", tunnelBastionService)
	assign("socks", tunnelSOCKSProxyService)
	assign("ssh_port", tunnelSSHPortRoute)
	assign("raw_tcp", tunnelRawTCPRoute)
	assign("unclassified", tunnelUnclassifiedRoute)
	assign("empty", tunnelEmptyServiceRoute)
	assign("bastion_mode", tunnelBastionModeRoute)
	assign("proxy_type", tunnelProxyTypeRoute)
	assign("socks5", tunnelSOCKS5Route)
	assign("judge_service", hostScanService)
	assign("judge_proxy", hostScanProxy)
	assign("judge_socks5", hostScanSOCKS5)
	assign("judge_home", hostScanHome)
	for _, spelling := range hostScanFalse {
		fmt.Fprintf(&b, "\tfalsy[%s] = 1\n", awkString(spelling))
	}
	for i, key := range hostScanConfigKeys {
		n := strconv.Itoa(i + 1)
		assign("key["+n+"]", strings.ToLower(key.name))
		assign("key_judge["+n+"]", key.judge)
	}
	fmt.Fprintf(&b, "\tnkeys = %d\n", len(hostScanConfigKeys))
	for i, setting := range hostScanUnitSettings {
		n := strconv.Itoa(i + 1)
		name := setting.name
		if !setting.env {
			name = "--?" + name
		}
		assign("name["+n+"]", name)
		fmt.Fprintf(&b, "\tis_env[%s] = %d\n", n, boolInt(setting.env))
		assign("name_judge["+n+"]", setting.judge)
	}
	fmt.Fprintf(&b, "\tnnames = %d\n", len(hostScanUnitSettings))
	// A value no allowlist accepts stands in for a form a line cannot hold
	// whole, such as a block scalar or an unterminated quote.
	b.WriteString("\tunreadable = sprintf(\"%c\", 1)\n}\n")
	b.WriteString(hostScanBody)
	return b.String()
}

// hostScanBody is the part of hostScanProgram that needs no constants. Its
// cases follow tunnelRouteToSSH, which follows cloudflared's
// ingress.validateIngress; url_reason follows net/url, which cloudflared
// parses origins with.
const hostScanBody = `function bad(label) {
	print label
	exit
}
function last_index(s, c,   i, n) {
	n = 0
	for (i = 1; i <= length(s); i++)
		if (substr(s, i, 1) == c)
			n = i
	return n
}
# url_reason judges a scheme://authority origin: the port is compared as a
# number, because cloudflared dials 0022 as port 22, and one above the
# largest TCP port cannot be placed, as in tunnelRouteToSSH.
function url_reason(v,   i, scheme, auth, host, p) {
	i = index(v, "://")
	scheme = tolower(substr(v, 1, i - 1))
	if (i < 2 || scheme !~ /^[a-z][a-z0-9+.-]*$/)
		return unclassified
	auth = substr(v, i + 3)
	sub(/[\/?#].*/, "", auth)
	sub(/.*@/, "", auth)
	if (substr(auth, 1, 1) == "[") {
		i = last_index(auth, "]")
		if (!i)
			return unclassified
		host = substr(auth, 2, i - 2)
		p = substr(auth, i + 1)
		if (p != "" && substr(p, 1, 1) != ":")
			return unclassified
		p = substr(p, 2)
	} else if ((i = last_index(auth, ":")) > 0) {
		host = substr(auth, 1, i - 1)
		p = substr(auth, i + 1)
	} else {
		host = auth
		p = ""
	}
	if (p !~ /^[0-9]*$/ || p + 0 > max_port)
		return unclassified
	if (scheme == tcp) {
		if (host == "" || p == "")
			return unclassified
	} else if (!(scheme in web)) {
		return raw_tcp
	} else if (host == "") {
		return unclassified
	} else if (p == "") {
		p = web[scheme]
	}
	if (p + 0 == port)
		return ssh_port
	return ""
}
function service_reason(v) {
	if (index(v, unix_prefix) == 1 || index(v, unix_tls_prefix) == 1 || index(v, status_prefix) == 1)
		return ""
	if (v == hello || v == hello_flag)
		return ""
	if (v == bastion || v == socks)
		return v
	if (v == "")
		return empty
	if (!index(v, "://"))
		return unclassified
	return url_reason(v)
}
# uncomment drops a YAML comment: a # at the start or after a blank, outside
# quotes. A stray quote keeps the rest of the line, which can only add
# settings to judge.
function uncomment(line,   i, c, q, prev) {
	q = ""
	prev = " "
	for (i = 1; i <= length(line); i++) {
		c = substr(line, i, 1)
		if (q != "") {
			if (c == q)
				q = ""
		} else if (c == "\"" || c == "'") {
			q = c
		} else if (c == "#" && (prev == " " || prev == "\t")) {
			return substr(line, 1, i - 1)
		}
		prev = c
	}
	return line
}
# flow_depth tells a key inside a { } or [ ] flow collection, where a comma
# or a closing bracket ends a plain value, from a block key, where the line
# end does.
function flow_depth(s,   i, c, q, d) {
	q = ""
	d = 0
	for (i = 1; i <= length(s); i++) {
		c = substr(s, i, 1)
		if (q != "") {
			if (c == q)
				q = ""
		} else if (c == "\"" || c == "'") {
			q = c
		} else if (c == "{" || c == "[") {
			d++
		} else if (c == "}" || c == "]") {
			d--
		}
	}
	return d
}
function scalar(r, flow,   q, v, i) {
	quoted = 0
	sub(/^[ \t]+/, "", r)
	q = substr(r, 1, 1)
	if (q == "\"" || q == "'") {
		v = substr(r, 2)
		i = index(v, q)
		if (!i || substr(v, i + 1, 1) == q)
			return unreadable
		v = substr(v, 1, i - 1)
		if (q == "\"" && index(v, "\\"))
			return unreadable
		quoted = 1
		return v
	}
	if (r ~ /^[|>&*!%@]/)
		return unreadable
	if (flow)
		sub(/[,}\]].*/, "", r)
	sub(/[ \t]+$/, "", r)
	return r
}
function config_setting(judge, v,   r) {
	if (judge == judge_service) {
		r = service_reason(v)
		if (r != "")
			bad(r)
	} else if (judge == judge_proxy) {
		if (v != "" || !quoted)
			bad(proxy_type)
	} else if (!(v in falsy)) {
		bad(judge == judge_socks5 ? socks5 : bastion_mode)
	}
}
function config_line(line,   lower, k, re, s, off, at) {
	sub(/\r$/, "", line)
	line = uncomment(line)
	lower = tolower(line)
	for (k = 1; k <= nkeys; k++) {
		re = "(^|[ \t{[,])[\"']?" key[k] "[\"']?[ \t]*:"
		s = lower
		off = 0
		while (match(s, re)) {
			at = off + RSTART + RLENGTH
			config_setting(key_judge[k], scalar(substr(line, at), flow_depth(substr(line, 1, at - 1)) > 0))
			off = at - 1
			s = substr(lower, at)
		}
	}
}
# setting_value reads a flag or variable value: up to the matching quote when
# the assignment or the value is quoted, else up to a blank.
function setting_value(r, delim,   q, i) {
	q = substr(r, 1, 1)
	if (delim == "\"" || delim == "'") {
		q = delim
	} else if (q == "\"" || q == "'") {
		r = substr(r, 2)
	} else {
		sub(/[ \t].*/, "", r)
		return r
	}
	i = index(r, q)
	return i ? substr(r, 1, i - 1) : unreadable
}
function unit_setting(judge, v, assigned,   r) {
	if (judge == judge_home) {
		print v
	} else if (judge == judge_service) {
		r = service_reason(v)
		if (r != "")
			bad(r)
	} else if (!assigned || (v != "" && !(v in falsy))) {
		bad(judge == judge_socks5 ? socks5 : bastion_mode)
	}
}
function settings_line(line,   i, s, off, at, delim, c, r) {
	for (i = 1; i <= nnames; i++) {
		if ((mode == home_mode) != (name_judge[i] == judge_home))
			continue
		s = line
		off = 0
		while (match(s, "(^|[ \t;\"'])" name[i])) {
			at = off + RSTART + RLENGTH
			delim = substr(line, off + RSTART, 1)
			off = at - 1
			s = substr(line, at)
			c = substr(s, 1, 1)
			if (c ~ /[A-Za-z0-9_-]/)
				continue
			if (c == "=") {
				unit_setting(name_judge[i], setting_value(substr(s, 2), delim), 1)
			} else if (!is_env[i]) {
				r = s
				sub(/^[ \t]+/, "", r)
				unit_setting(name_judge[i], name_judge[i] == judge_service ? setting_value(r, delim) : "", 0)
			}
		}
	}
}
mode == config_mode {
	config_line($0)
	next
}
{
	settings_line($0)
}
`

// awkString quotes s as an awk string literal.
func awkString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// boolInt spells a bool the way awk tests it.
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
