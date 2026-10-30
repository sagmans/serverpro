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
	// sudoersSectionMarker, sudoCommandsSectionMarker,
	// sudoersPathsSectionMarker, adminUIDSectionMarker, and
	// accountsSectionMarker split the privileged accounts probe output:
	// sudo's own reading of the rules, the host's verdict on each listed
	// command and on each path a Defaults option names, the admin's uid, and
	// the findings the host grades itself.
	sudoersSectionMarker      = "--- sudoers"
	sudoCommandsSectionMarker = "--- sudo commands"
	sudoersPathsSectionMarker = "--- sudoers paths"
	adminUIDSectionMarker     = "--- admin uid"
	accountsSectionMarker     = "--- accounts"
	// sudoCommandVerdictSeparator splits a path from the host's verdict on
	// it, and a Defaults path's kind from the path. A path read out of the
	// JSON cannot hold a raw tab, because JSON escapes control characters.
	sudoCommandVerdictSeparator = "\t"
	// sudoersPathDirectory and sudoersPathFile are the kinds of path a
	// Defaults option names. The host checks that the path resolves to that
	// kind, and the words are also how its verdict spells the kind.
	sudoersPathDirectory = "directory"
	sudoersPathFile      = "file"
	// sudoersPathEscape starts a sudo escape (%{user}, %{seq}) that iolog_dir
	// expands per command, so the path the host would check is not the one
	// sudo writes.
	sudoersPathEscape = "%"
	// sudoersSecurePath is the option that replaces the caller's PATH for
	// every command, and sudoersSecurePathSeparator splits its directories
	// the way PATH splits.
	sudoersSecurePath          = "secure_path"
	sudoersSecurePathSeparator = ":"
	// sudoersListOperation is the JSON key that says how a list option
	// changes its list. sudoersListOperationAdd and sudoersListOperationAssign
	// are the "+=" and "=" forms, which can add names, and
	// sudoersListOperationRemove is the "-=" form, which can only take names
	// out.
	sudoersListOperation       = "operation"
	sudoersListOperationAdd    = "list_add"
	sudoersListOperationAssign = "list_assign"
	sudoersListOperationRemove = "list_remove"
	// sudoCommandSafe is the host's verdict on a path that only root can
	// change and that does not resolve to a root-equivalent command.
	sudoCommandSafe = "ok"
	// sudoersFindingSubject names where a rule comes from. sudo's parser
	// merges every included file and does not say which one held a rule.
	sudoersFindingSubject = "sudoers"
	// sudoersAllCommands is the command sudo reads as every command.
	sudoersAllCommands = "ALL"
	// sudoersEnvWildcard ends an env_keep or env_check entry that keeps every
	// name it starts.
	sudoersEnvWildcard = "*"
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

// sudoersDefaultsGrade says which values of one Defaults option doctor
// passes. The grade is an allowlist: an option sudoersSafeDefaults does not
// name, or a value its grade does not accept, fails, so an option sudo adds
// later, or a value doctor cannot read, is a gap until doctor knows it.
type sudoersDefaultsGrade int

const (
	// defaultsEitherState is a flag that neither state lets a sudo user run
	// more, run it with code or files the user chooses, or skip
	// authentication: logging, mail, prompts, and terminal handling.
	defaultsEitherState sudoersDefaultsGrade = iota
	// defaultsOnlyOn and defaultsOnlyOff are flags that are safe in one
	// state only, such as env_reset on or setenv off.
	defaultsOnlyOn
	defaultsOnlyOff
	// defaultsNumber is a count or a timeout, or the option turned off.
	defaultsNumber
	// defaultsWord is a syslog facility or priority name, or off.
	defaultsWord
	// defaultsText is a message sudo prints to the caller.
	defaultsText
	// defaultsTuple is one of the words sudoersDefaultsTuples lists, or a
	// flag.
	defaultsTuple
	// defaultsDirectoryList, defaultsDirectory, and defaultsFile name paths
	// sudo searches, reads, or writes as root. Each one must resolve to a
	// path only root can change, by the host's verdict; logfile may also be
	// off.
	defaultsDirectoryList
	defaultsDirectory
	defaultsFile
	// defaultsEnvNames is env_keep or env_check: every name it adds must be
	// one sudoersKeptEnvNames covers.
	defaultsEnvNames
	// defaultsAnyList is env_delete, which only takes names away, and env_reset
	// on already drops them.
	defaultsAnyList
)

// sudoersSafeDefaults is every Defaults option doctor passes, with the
// values it may hold (see sudoersDefaultsGrade). The names follow sudo's
// option table (def_data.in, see WEB_SOURCES.md). It covers Ubuntu's stock
// /etc/sudoers (env_reset, mail_badpass, secure_path, use_pty), the verifypw
// entry serverpro's bootstrap drop-in sets, and the usual logging and
// prompt hardening.
var sudoersSafeDefaults = map[string]sudoersDefaultsGrade{
	"use_pty": defaultsEitherState, "requiretty": defaultsEitherState,
	"mail_always": defaultsEitherState, "mail_badpass": defaultsEitherState,
	"mail_no_user": defaultsEitherState, "mail_no_host": defaultsEitherState,
	"mail_no_perms": defaultsEitherState, "mail_all_cmnds": defaultsEitherState,
	"log_host": defaultsEitherState, "log_year": defaultsEitherState,
	"log_input": defaultsEitherState, "log_output": defaultsEitherState,
	"log_stdin": defaultsEitherState, "log_stdout": defaultsEitherState,
	"log_stderr": defaultsEitherState, "log_ttyin": defaultsEitherState,
	"log_ttyout": defaultsEitherState, "compress_io": defaultsEitherState,
	"iolog_flush": defaultsEitherState, "log_allowed": defaultsEitherState,
	"log_denied": defaultsEitherState, "log_subcmds": defaultsEitherState,
	"log_exit_status": defaultsEitherState, "syslog_pid": defaultsEitherState,
	"ignore_audit_errors": defaultsEitherState, "ignore_iolog_errors": defaultsEitherState,
	"ignore_logfile_errors": defaultsEitherState, "insults": defaultsEitherState,
	"pwfeedback": defaultsEitherState, "passprompt_override": defaultsEitherState,
	"long_otp_prompt": defaultsEitherState, "path_info": defaultsEitherState,
	"set_utmp": defaultsEitherState, "utmp_runas": defaultsEitherState,
	"noexec": defaultsEitherState, "intercept": defaultsEitherState,
	"intercept_verify": defaultsEitherState, "intercept_authenticate": defaultsEitherState,
	"set_logname": defaultsEitherState, "set_home": defaultsEitherState,
	"always_set_home": defaultsEitherState, "fqdn": defaultsEitherState,
	"root_sudo": defaultsEitherState, "rootpw": defaultsEitherState,
	"pam_session": defaultsEitherState, "pam_setcred": defaultsEitherState,
	"match_group_by_gid": defaultsEitherState,
	// Off, each lets a caller pass the caller's environment, skip the
	// password or account checks, reuse one terminal's credentials in
	// another, or edit through a link it controls.
	"env_reset": defaultsOnlyOn, "authenticate": defaultsOnlyOn,
	"ignore_dot": defaultsOnlyOn, "tty_tickets": defaultsOnlyOn,
	"sudoedit_checkdir": defaultsOnlyOn, "pam_acct_mgmt": defaultsOnlyOn,
	// On, each lets a caller choose the environment, keep its own uid,
	// groups, file descriptors, or umask, run setuid programs from an
	// intercepted command, match accounts by case or unknown id, store
	// passwords in the I/O log, or drop the local rules.
	"setenv": defaultsOnlyOff, "stay_setuid": defaultsOnlyOff,
	"closefrom_override": defaultsOnlyOff, "umask_override": defaultsOnlyOff,
	"visiblepw": defaultsOnlyOff, "targetpw": defaultsOnlyOff,
	"runaspw": defaultsOnlyOff, "preserve_groups": defaultsOnlyOff,
	"sudoedit_follow": defaultsOnlyOff, "intercept_allow_setid": defaultsOnlyOff,
	"log_passwords": defaultsOnlyOff, "runas_allow_unknown_id": defaultsOnlyOff,
	"case_insensitive_user": defaultsOnlyOff, "case_insensitive_group": defaultsOnlyOff,
	"ignore_local_sudoers": defaultsOnlyOff, "shell_noargs": defaultsOnlyOff,
	"passwd_tries": defaultsNumber, "passwd_timeout": defaultsNumber,
	"timestamp_timeout": defaultsNumber, "loglinelen": defaultsNumber,
	"syslog_maxlen": defaultsNumber, "command_timeout": defaultsNumber,
	"syslog": defaultsWord, "syslog_goodpri": defaultsWord, "syslog_badpri": defaultsWord,
	"badpass_message": defaultsText, "passprompt": defaultsText, "authfail_message": defaultsText,
	"lecture": defaultsTuple, "listpw": defaultsTuple, "verifypw": defaultsTuple,
	"timestamp_type":  defaultsTuple,
	sudoersSecurePath: defaultsDirectoryList, "iolog_dir": defaultsDirectory,
	"logfile":  defaultsFile,
	"env_keep": defaultsEnvNames, "env_check": defaultsEnvNames,
	"env_delete": defaultsAnyList,
}

// sudoersDefaultsTuples are the words a defaultsTuple option may hold. The
// listpw and verifypw words only decide when sudo -l and sudo -v ask for a
// password, and neither runs a command. timestamp_type leaves out global,
// which lets one terminal's credentials serve every other.
var sudoersDefaultsTuples = map[string][]string{
	"lecture":        {"never", "once", "always"},
	"listpw":         {"all", "always", "any", "never"},
	"verifypw":       {"all", "always", "any", "never"},
	"timestamp_type": {"tty", "ppid", "kernel"},
}

// sudoersTupleFlags are the defaultsTuple options sudo's option table also
// types as a flag (T_TUPLE|T_BOOL), so the JSON may hold true or false for
// them; for timestamp_type a flag is a value doctor cannot read.
var sudoersTupleFlags = []string{"lecture", "listpw", "verifypw"}

// sudoersDefaultsReasons say what turning a flag to its unsafe state
// opens, for the flags an operator most often changes by mistake. A value
// that is not a flag keeps the generic reason, because it may fail for
// another cause.
var sudoersDefaultsReasons = map[string]string{
	"setenv":          "lets a sudo user set LD_PRELOAD for a root command",
	"env_reset":       "keeps the caller's environment for a root command",
	"authenticate":    "lets every rule run without a password",
	sudoersSecurePath: "lets a root command search the caller's PATH",
}

// sudoersPathOptions are the defaultsDirectoryList, defaultsDirectory, and
// defaultsFile options in a fixed order, so the host command doctor builds
// from them is the same on every run.
var sudoersPathOptions = func() []string {
	var names []string
	for name, grade := range sudoersSafeDefaults {
		if grade == defaultsDirectoryList || grade == defaultsDirectory || grade == defaultsFile {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}()

// sudoersKeptEnvNames are the variables env_keep and env_check may add: the
// names sudo itself keeps or checks when env_reset is on (env.c's
// initial_keepenv_table and initial_checkenv_table, see WEB_SOURCES.md),
// less PATH. A kept PATH makes a root command that runs a helper by bare
// name run the caller's program wherever secure_path does not apply. Any
// other name may make a shell, the loader, an interpreter, or a library
// read code or files the caller chooses. A trailing "*" is a sudo wildcard.
var sudoersKeptEnvNames = []string{
	"COLORS", "DISPLAY", "HOSTNAME", "KRB5CCNAME", "LS_COLORS", "PS1", "PS2",
	"XAUTHORITY", "XAUTHORIZATION", "XDG_CURRENT_DESKTOP",
	"COLORTERM", "LANG", "LANGUAGE", "LC_*", "LINGUAS", "TERM", "TZ",
}

// sudoersNumberPattern is a count, a timeout in minutes, or a command
// timeout with units (1h30m), the forms sudo's parser accepts;
// sudoersWordPattern is a syslog facility or priority name. Neither form
// can name a file or a program.
var (
	sudoersNumberPattern = regexp.MustCompile(`^([0-9]+([.][0-9]+)?|([0-9]+[dhmsDHMS])+)$`)
	sudoersWordPattern   = regexp.MustCompile(`^[a-z0-9]+$`)
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
		sections, err := splitProbeSections(out, sudoersSectionMarker, sudoCommandsSectionMarker, sudoersPathsSectionMarker, adminUIDSectionMarker, accountsSectionMarker)
		if err != nil {
			return "", err
		}
		policy, err := parseSudoersPolicy(sections[sudoersSectionMarker])
		if err != nil {
			return "", err
		}
		verdicts, err := parseHostVerdicts(sections[sudoCommandsSectionMarker], 2, "sudo command")
		if err != nil {
			return "", err
		}
		paths, err := parseHostVerdicts(sections[sudoersPathsSectionMarker], 3, "sudoers path")
		if err != nil {
			return "", err
		}
		exempt := append(slices.Clone(sudoersExemptPrincipals), admin, "#"+rootUID)
		if uid := strings.TrimSpace(sections[adminUIDSectionMarker]); uid != "" {
			exempt = append(exempt, "#"+uid)
		}
		findings := append(policy.defaultsFindings(paths), policy.ruleFindings(exempt, verdicts)...)
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

// parseHostVerdicts reads the host's tab-separated verdict lines: fields-1
// key fields (a command path, or a Defaults path's kind and path) and the
// verdict, which may itself hold a tab from a file name the host found. The
// key joins the key fields with the same tab, so Go looks a path up the way
// the host printed it.
func parseHostVerdicts(section string, fields int, what string) (map[string]string, error) {
	verdicts := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, sudoCommandVerdictSeparator, fields)
		if len(parts) < fields || slices.Contains(parts, "") {
			return nil, fmt.Errorf("could not evaluate privileged accounts: unreadable %s check %q", what, line)
		}
		verdicts[strings.Join(parts[:fields-1], sudoCommandVerdictSeparator)] = parts[fields-1]
	}
	return verdicts, nil
}

// defaultsFindings grades every Defaults option against sudoersSafeDefaults,
// whatever it is bound to: the JSON does not tell a user binding from a
// runas binding, so a binding that names root may apply to every command run
// as root. paths holds the host's verdict on each path a path option names.
// A policy with no unbound secure_path fails too, because a root command
// then searches the caller's PATH, which env_reset keeps.
func (p sudoersPolicy) defaultsFindings(paths map[string]string) []string {
	var findings []string
	securePath := false
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
			if _, named := option[sudoersSecurePath]; named && len(defaults.Binding) == 0 {
				securePath = true
			}
			for _, gap := range defaultsOptionGaps(option, paths) {
				findings = append(findings, driftFailPrefix+sudoersFindingSubject+" "+subject+" "+gap)
			}
		}
	}
	if !securePath {
		findings = append(findings, driftFailPrefix+sudoersFindingSubject+" Defaults sets no "+sudoersSecurePath+", so a root command searches the caller's PATH")
	}
	return findings
}

// defaultsOptionGaps grades one JSON option object: one option, plus the
// operation key for a list change. An operation doctor does not know may
// add names in a way it cannot see, so it fails the option.
func defaultsOptionGaps(option map[string]any, paths map[string]string) []string {
	operation, changesList := option[sudoersListOperation]
	if changesList && operation != sudoersListOperationAdd && operation != sudoersListOperationAssign && operation != sudoersListOperationRemove {
		return []string{fmt.Sprintf("changes a list with operation %v, which doctor cannot read", operation)}
	}
	var gaps []string
	for name, value := range option {
		if name != sudoersListOperation {
			gaps = append(gaps, defaultsValueGaps(name, value, operation == sudoersListOperationRemove, paths)...)
		}
	}
	return gaps
}

// defaultsValueGaps grades one option's value by its sudoersSafeDefaults
// grade. Any option or value the grade does not accept fails with a generic
// reason, or with the specific one sudoersDefaultsReasons gives.
func defaultsValueGaps(name string, value any, removes bool, paths map[string]string) []string {
	grade, known := sudoersSafeDefaults[name]
	flag, isFlag := value.(bool)
	text, isText := value.(string)
	switch {
	case !known:
	case grade == defaultsEitherState && isFlag,
		grade == defaultsOnlyOn && isFlag && flag,
		grade == defaultsOnlyOff && isFlag && !flag,
		grade == defaultsNumber && (isFlag && !flag || isText && sudoersNumberPattern.MatchString(text)),
		grade == defaultsWord && (isFlag && !flag || isText && sudoersWordPattern.MatchString(text)),
		grade == defaultsText && isText,
		grade == defaultsTuple && (isFlag && slices.Contains(sudoersTupleFlags, name) || isText && slices.Contains(sudoersDefaultsTuples[name], text)),
		grade == defaultsAnyList && (isFlag || stringList(value)),
		grade == defaultsEnvNames && (isFlag && !flag || removes):
		return nil
	case grade == defaultsDirectoryList && isText && text != "":
		var gaps []string
		for _, directory := range strings.Split(text, sudoersSecurePathSeparator) {
			gaps = append(gaps, defaultsPathGaps(name, sudoersPathDirectory, directory, paths)...)
		}
		return gaps
	case (grade == defaultsDirectory || grade == defaultsFile) && isFlag && !flag:
		return nil
	case grade == defaultsDirectory && isText:
		return defaultsPathGaps(name, sudoersPathDirectory, text, paths)
	case grade == defaultsFile && isText:
		return defaultsPathGaps(name, sudoersPathFile, text, paths)
	case grade == defaultsEnvNames && stringList(value):
		var gaps []string
		for _, entry := range value.([]any) {
			if !keptEnvName(entry.(string)) {
				gaps = append(gaps, fmt.Sprintf("adds %q to %s, which keeps it in the environment of a root command", entry, name))
			}
		}
		return gaps
	}
	reason, specific := sudoersDefaultsReasons[name]
	if !specific || !isFlag {
		reason = "doctor does not grade as safe"
	}
	return []string{describeDefaultsValue(name, value) + ", which " + reason}
}

// describeDefaultsValue spells an option the way a finding names it. Values
// are quoted, so a value with a newline cannot split the finding.
func describeDefaultsValue(name string, value any) string {
	switch value := value.(type) {
	case bool:
		if value {
			return "turns on " + name
		}
		return "turns off " + name
	case string:
		return fmt.Sprintf("sets %s to %q", name, value)
	default:
		return fmt.Sprintf("sets %s in a form doctor cannot read", name)
	}
}

// stringList reports whether a JSON value is a list of strings, the only
// list form doctor grades.
func stringList(value any) bool {
	entries, ok := value.([]any)
	if !ok {
		return false
	}
	for _, entry := range entries {
		if _, ok := entry.(string); !ok {
			return false
		}
	}
	return true
}

// keptEnvName reports whether every name an env_keep or env_check entry
// keeps is one sudoersKeptEnvNames covers. The part after "=" only limits
// the value sudo keeps, so the name alone decides. A wildcard entry passes
// only inside an allowed wildcard ("LC_*" or "LC_C*" under "LC_*"), because
// "L*" would also keep LD_PRELOAD.
func keptEnvName(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	prefix, wildcard := strings.CutSuffix(name, sudoersEnvWildcard)
	for _, kept := range sudoersKeptEnvNames {
		keptPrefix, keptWildcard := strings.CutSuffix(kept, sudoersEnvWildcard)
		if keptWildcard && strings.HasPrefix(prefix, keptPrefix) || !wildcard && !keptWildcard && name == kept {
			return true
		}
	}
	return false
}

// defaultsPathGaps grades one path a path option names by the host's
// verdict. A relative or empty entry makes sudo resolve it from the
// caller's working directory, and a sudo escape expands per command to a
// path the host never checked, so both fail without a verdict.
func defaultsPathGaps(name, kind, path string, paths map[string]string) []string {
	subject := fmt.Sprintf("names %q in %s, ", path, name)
	switch verdict, checked := paths[kind+sudoCommandVerdictSeparator+path]; {
	case !strings.HasPrefix(path, "/"):
		return []string{subject + "which is not an absolute path"}
	case strings.Contains(path, sudoersPathEscape):
		return []string{subject + "which holds a sudo escape doctor cannot check"}
	case !checked:
		return []string{subject + "which the host did not check"}
	case verdict != sudoCommandSafe:
		return []string{subject + verdict}
	}
	return nil
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
