package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	// sudoersSectionMarker, sudoCommandsSectionMarker, adminUIDSectionMarker,
	// and accountsSectionMarker split the privileged accounts probe output:
	// sudo's own reading of the rules, the host's verdict on each listed
	// path, the admin's uid, and the findings the host grades itself.
	sudoersSectionMarker      = "--- sudoers"
	sudoCommandsSectionMarker = "--- sudo commands"
	adminUIDSectionMarker     = "--- admin uid"
	accountsSectionMarker     = "--- accounts"
	// sudoCommandVerdictSeparator splits a path from the host's verdict on
	// it. A path read out of the JSON cannot hold a raw tab, because JSON
	// escapes control characters.
	sudoCommandVerdictSeparator = "\t"
	// sudoCommandSafe is the host's verdict on a path that only root can
	// change and that does not resolve to a root-equivalent command.
	sudoCommandSafe = "ok"
	// sudoersFindingSubject names where a rule comes from. sudo's parser
	// merges every included file and does not say which one held a rule.
	sudoersFindingSubject = "sudoers"
	// sudoersAllCommands is the command sudo reads as every command.
	sudoersAllCommands = "ALL"
	// sudoersListOperationRemove is the JSON operation of a "-=" list
	// change, which can only take names out of env_keep.
	sudoersListOperationRemove = "list_remove"
	// sudoersCommandWildcards are the characters that make a command path a
	// pattern; a pattern can match a file the account controls.
	sudoersCommandWildcards = "*?["
	// rootUID is the uid sudoers writes as "#0".
	rootUID = "0"
)

// sudoersExemptPrincipals are the principals the privileged accounts check
// does not grade as sudoers grants: root, and the sudo and admin groups,
// whose members the group scan reports one by one.
var sudoersExemptPrincipals = []string{"root", "%sudo", "%admin"}

// sudoersSafeOptions are the per-command options that cannot widen what a
// rule grants: they ask for a password, block or log what the command does,
// limit when it runs, or change only its working directory. Any other option
// fails the rule, so an option sudo adds later is graded as a gap until
// doctor knows it. setenv is graded on its own, and runchroot is missing on
// purpose: a chroot the account can write chooses the binary sudo runs.
var sudoersSafeOptions = []string{
	"authenticate", "noexec", "intercept", "send_mail", "log_input",
	"log_output", "sudoedit_follow", "runcwd", "notbefore", "notafter",
	"command_timeout",
}

// unsafeEnvPrefixes and unsafeEnvNames are the variables that make the
// dynamic loader or an interpreter load code the caller chooses. Defaults
// that keep one of them lets a sudo user run its own code as root through
// any command a rule lists.
var (
	unsafeEnvPrefixes = []string{"LD_", "PYTHON", "PERL5", "RUBY"}
	unsafeEnvNames    = []string{"BASH_ENV", "ENV", "NODE_OPTIONS", "GCONV_PATH"}
)

// commandNameExpressions are commandNamePatterns compiled once for the Go
// side of the root-equivalent check, so Go and the host's sed agree.
var commandNameExpressions = func() []*regexp.Regexp {
	expressions := make([]*regexp.Regexp, len(commandNamePatterns))
	for i, pattern := range commandNamePatterns {
		expressions[i] = regexp.MustCompile(pattern)
	}
	return expressions
}()

// sudoersPolicy is the part of cvtsudoers' JSON output that grants or
// widens sudo. User_Specs is a pointer so a missing section, which a sudo
// change to the format would cause, fails closed instead of reading as a
// host without rules.
type sudoersPolicy struct {
	Defaults  []sudoersDefaults  `json:"Defaults"`
	UserSpecs *[]sudoersUserSpec `json:"User_Specs"`
}

// sudoersDefaults is one Defaults line. Its binding lists users, hosts, or
// commands, but the JSON spells a user binding (Defaults:root) and a runas
// binding (Defaults>root) the same way, so doctor grades every binding as
// if it reached the accounts it reviews.
type sudoersDefaults struct {
	Binding []sudoersMember  `json:"Binding"`
	Options []map[string]any `json:"Options"`
}

// sudoersUserSpec is one rule after alias expansion: who, on which hosts,
// and the command specs. Hosts are not graded, because a rule for another
// host name applies after one hostname change.
type sudoersUserSpec struct {
	Users     []sudoersMember   `json:"User_List"`
	CmndSpecs []sudoersCmndSpec `json:"Cmnd_Specs"`
}

// sudoersCmndSpec is one command list with its tags and options. The runas
// list is not graded: a rule that runs a command as another account still
// hands out that account's access, and doctor reviews every grant.
type sudoersCmndSpec struct {
	Options  []map[string]any `json:"Options"`
	Commands []sudoersMember  `json:"Commands"`
}

// sudoersMember is one entry of a user, host, binding, or command list: a
// single typed name (username, usergroup, userid, command, ...) plus an
// optional negation and command digest.
type sudoersMember map[string]any

// sudoersMemberSpellings map a JSON member type to the sudoers prefix that
// writes it, so findings show principals the way an operator wrote them.
// The order is fixed because a command member also carries digest keys.
var sudoersMemberSpellings = []struct{ kind, prefix string }{
	{"username", ""}, {"usergroup", "%"}, {"userid", "#"}, {"usergid", "%#"},
	{"netgroup", "+"}, {"nonunixgroup", "%:"}, {"nonunixgid", "%:#"},
	{"hostname", ""}, {"command", ""},
}

func (m sudoersMember) negated() bool {
	negated, _ := m["negated"].(bool)
	return negated
}

// String spells the member as sudoers would. A member type the list does
// not know keeps its JSON name, so a new type still shows in a finding and
// is graded like any other principal.
func (m sudoersMember) String() string {
	for _, spelling := range sudoersMemberSpellings {
		if value, ok := m[spelling.kind]; ok {
			return spelling.prefix + fmt.Sprint(value)
		}
	}
	kinds := make([]string, 0, len(m))
	for kind := range m {
		if kind != "negated" {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return "(empty)"
	}
	sort.Strings(kinds)
	return kinds[0] + "=" + fmt.Sprint(m[kinds[0]])
}

// command returns the command text, and false when the entry is not a
// command at all, which the grader treats as unlimited.
func (m sudoersMember) command() (string, bool) {
	command, ok := m["command"].(string)
	return command, ok
}

// gradePrivilegedAccounts returns the grader for privilegedAccountsCommand's
// output. sudo's own parser (cvtsudoers) reads the rules on the host, so
// includes, aliases, continued lines, negation, uid users, and user lists
// mean exactly what they mean to sudo; Go grades the parsed rules because
// the host has no JSON tool doctor can rely on. Any section it cannot read
// fails the check as a probe failure, never as a host with nothing to
// report.
func gradePrivilegedAccounts(admin string) func(string) (string, error) {
	return func(out string) (string, error) {
		sections, err := splitProbeSections(out, sudoersSectionMarker, sudoCommandsSectionMarker, adminUIDSectionMarker, accountsSectionMarker)
		if err != nil {
			return "", err
		}
		policy, err := parseSudoersPolicy(sections[sudoersSectionMarker])
		if err != nil {
			return "", err
		}
		verdicts, err := parseSudoCommandVerdicts(sections[sudoCommandsSectionMarker])
		if err != nil {
			return "", err
		}
		exempt := append(slices.Clone(sudoersExemptPrincipals), admin, "#"+rootUID)
		if uid := strings.TrimSpace(sections[adminUIDSectionMarker]); uid != "" {
			exempt = append(exempt, "#"+uid)
		}
		findings := append(policy.defaultsFindings(), policy.ruleFindings(exempt, verdicts)...)
		var details []string
		for _, line := range strings.Split(sections[accountsSectionMarker], "\n") {
			switch line = strings.TrimSpace(line); {
			case line == "":
			case strings.HasPrefix(line, driftFailPrefix), strings.HasPrefix(line, driftWarnPrefix):
				findings = append(findings, line)
			default:
				details = append(details, line)
			}
		}
		sort.Strings(findings)
		return strings.Join(append(slices.Compact(findings), details...), "\n"), nil
	}
}

// splitProbeSections cuts probe output at whole marker lines, which must
// all appear in order: a missing one means the probe stopped part way. No
// section body can hold a marker line, because JSON escapes newlines and a
// path line always carries a tab.
func splitProbeSections(out string, markers ...string) (map[string]string, error) {
	sections := make(map[string][]string, len(markers))
	current, next := "", 0
	for _, line := range strings.Split(out, "\n") {
		if next < len(markers) && line == markers[next] {
			current = markers[next]
			sections[current] = []string{}
			next++
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	if next < len(markers) {
		return nil, fmt.Errorf("could not evaluate privileged accounts: probe output has no %q section", markers[next])
	}
	joined := make(map[string]string, len(markers))
	for marker, lines := range sections {
		joined[marker] = strings.Join(lines, "\n")
	}
	return joined, nil
}

// parseSudoersPolicy fails on anything but one JSON document with a
// User_Specs section, so a truncated or changed format never reads as a host
// whose sudoers grants nothing.
func parseSudoersPolicy(document string) (sudoersPolicy, error) {
	var policy sudoersPolicy
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("could not evaluate privileged accounts: cannot read sudo's parse of the sudoers rules: %w", err)
	}
	if decoder.More() {
		return policy, errors.New("could not evaluate privileged accounts: sudo's parse of the sudoers rules holds more than one document")
	}
	if policy.UserSpecs == nil {
		return policy, errors.New("could not evaluate privileged accounts: sudo's parse of the sudoers rules has no User_Specs")
	}
	return policy, nil
}

// parseSudoCommandVerdicts reads one "path<TAB>verdict" line per listed
// path the host checked.
func parseSudoCommandVerdicts(section string) (map[string]string, error) {
	verdicts := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if line == "" {
			continue
		}
		path, verdict, found := strings.Cut(line, sudoCommandVerdictSeparator)
		if !found || path == "" || verdict == "" {
			return nil, fmt.Errorf("could not evaluate privileged accounts: unreadable sudo command check %q", line)
		}
		verdicts[path] = verdict
	}
	return verdicts, nil
}

// defaultsFindings fails every Defaults entry that lets a sudo user carry
// code into a root command through the environment, whatever it is bound to:
// the JSON does not tell a user binding from a runas binding, so a binding
// that names root may apply to every command run as root.
func (p sudoersPolicy) defaultsFindings() []string {
	var findings []string
	for _, defaults := range p.Defaults {
		subject := "Defaults"
		if len(defaults.Binding) > 0 {
			names := make([]string, len(defaults.Binding))
			for i, member := range defaults.Binding {
				names[i] = member.String()
				if member.negated() {
					names[i] = "!" + names[i]
				}
			}
			subject += " for " + strings.Join(names, ", ")
		}
		for _, option := range defaults.Options {
			for _, gap := range unsafeDefaultsOption(option) {
				findings = append(findings, driftFailPrefix+sudoersFindingSubject+" "+subject+" "+gap)
			}
		}
	}
	return findings
}

// unsafeDefaultsOption names what one Defaults option opens. env_keep that
// is not a list of strings is a format doctor cannot read, so it fails too.
func unsafeDefaultsOption(option map[string]any) []string {
	var gaps []string
	if setenv, ok := option["setenv"].(bool); ok && setenv {
		gaps = append(gaps, "turns on setenv, which lets a sudo user set LD_PRELOAD for a root command")
	}
	if reset, ok := option["env_reset"].(bool); ok && !reset {
		gaps = append(gaps, "turns off env_reset, which keeps the caller's environment for a root command")
	}
	keep, present := option["env_keep"]
	if !present || option["operation"] == sudoersListOperationRemove {
		return gaps
	}
	names, ok := keep.([]any)
	if !ok {
		return append(gaps, "sets env_keep in a form doctor cannot read")
	}
	for _, name := range names {
		text, ok := name.(string)
		if !ok || unsafeEnvName(text) {
			gaps = append(gaps, fmt.Sprintf("keeps %v in the environment of a root command", name))
		}
	}
	return gaps
}

// unsafeEnvName matches an env_keep entry against unsafeEnvPrefixes and
// unsafeEnvNames. A trailing "*" is a sudo wildcard that keeps every name it
// starts, so "L*" keeps LD_PRELOAD too.
func unsafeEnvName(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	if prefix, wildcard := strings.CutSuffix(name, "*"); wildcard {
		return slices.ContainsFunc(unsafeEnvPrefixes, func(unsafe string) bool {
			return strings.HasPrefix(unsafe, prefix) || strings.HasPrefix(prefix, unsafe)
		}) || slices.ContainsFunc(unsafeEnvNames, func(unsafe string) bool { return strings.HasPrefix(unsafe, prefix) })
	}
	return slices.Contains(unsafeEnvNames, name) || slices.ContainsFunc(unsafeEnvPrefixes, func(unsafe string) bool { return strings.HasPrefix(name, unsafe) })
}

// ruleFindings grades every command spec for every principal that is not
// exempt. A negated principal grants nothing, so it is skipped.
func (p sudoersPolicy) ruleFindings(exempt []string, verdicts map[string]string) []string {
	var findings []string
	for _, spec := range *p.UserSpecs {
		var principals []string
		for _, user := range spec.Users {
			if name := user.String(); !user.negated() && !slices.Contains(exempt, name) {
				principals = append(principals, name)
			}
		}
		if len(principals) == 0 {
			continue
		}
		for _, cmndSpec := range spec.CmndSpecs {
			for _, gap := range cmndSpec.gaps(verdicts) {
				for _, principal := range principals {
					findings = append(findings, gap.prefix+sudoersFindingSubject+" grants sudo to "+principal+gap.text)
				}
			}
		}
	}
	return findings
}

// sudoersGap is one graded reason, with the severity prefix it reports at.
type sudoersGap struct {
	prefix, text string
}

// gaps grades one command spec. A rule limited to absolute paths that only
// root can change is how a CI deploy user gets the few commands it needs,
// so it warns and names them; anything that may run every command, a
// root-equivalent command, a path that may hide one, or an option that can
// widen the grant fails.
func (s sudoersCmndSpec) gaps(verdicts map[string]string) []sudoersGap {
	var listed []string
	for _, entry := range s.Commands {
		command, ok := entry.command()
		if !ok {
			return []sudoersGap{{driftFailPrefix, " without a command limit"}}
		}
		if entry.negated() {
			continue
		}
		path, _, _ := strings.Cut(command, " ")
		if command == sudoersAllCommands || !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.ContainsAny(path, sudoersCommandWildcards) {
			return []sudoersGap{{driftFailPrefix, " without a command limit"}}
		}
		if !slices.Contains(listed, path) {
			listed = append(listed, path)
		}
	}
	var gaps []sudoersGap
	var rooted []string
	for _, path := range listed {
		if rootEquivalentName(path) {
			rooted = append(rooted, path)
		}
	}
	if len(rooted) > 0 {
		gaps = append(gaps, sudoersGap{driftFailPrefix, " for root-equivalent commands: " + strings.Join(rooted, " ")})
	}
	gaps = append(gaps, s.optionGaps()...)
	for _, path := range listed {
		// A listed root-equivalent name already fails above; its host
		// verdict would only repeat the same finding.
		if slices.Contains(rooted, path) {
			continue
		}
		switch verdict, checked := verdicts[path]; {
		case strings.Contains(path, `\`):
			gaps = append(gaps, sudoersGap{driftFailPrefix, " for " + path + ", whose path holds a backslash that sudo and doctor may read differently"})
		case !checked:
			gaps = append(gaps, sudoersGap{driftFailPrefix, " for " + path + ", which the host did not check"})
		case verdict != sudoCommandSafe:
			gaps = append(gaps, sudoersGap{driftFailPrefix, " for " + path + ", " + verdict})
		}
	}
	if len(gaps) == 0 && len(listed) > 0 {
		gaps = append(gaps, sudoersGap{driftWarnPrefix, sudoersListedSeparator + strings.Join(listed, " ")})
	}
	return gaps
}

// optionGaps fails SETENV and every option that sudoersSafeOptions does not
// list.
func (s sudoersCmndSpec) optionGaps() []sudoersGap {
	var gaps []sudoersGap
	for _, option := range s.Options {
		for name, value := range option {
			switch {
			case name == "setenv":
				if enabled, _ := value.(bool); enabled {
					gaps = append(gaps, sudoersGap{driftFailPrefix, " with SETENV, which lets it set LD_PRELOAD for a root command"})
				}
			case !slices.Contains(sudoersSafeOptions, name):
				gaps = append(gaps, sudoersGap{driftFailPrefix, fmt.Sprintf(" with option %s=%v, which doctor does not grade as safe", name, value)})
			}
		}
	}
	return gaps
}

// rootEquivalentName reports whether a listed path's base name, normalized
// the way the host normalizes a resolved path, is a root-equivalent command.
func rootEquivalentName(path string) bool {
	base := path[strings.LastIndex(path, "/")+1:]
	for _, expression := range commandNameExpressions {
		base = expression.ReplaceAllString(base, "")
	}
	if slices.Contains(rootEquivalentCommands, base) {
		return true
	}
	for _, prefix := range rootEquivalentPrefixes {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}
