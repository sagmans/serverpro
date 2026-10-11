package doctor

import (
	"context"
	"errors"
	"strings"

	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/remote"
)

// A repair that already ran in this doctor run and still left a check failing
// is reported once, pointing at the first failure, whose repair output says
// why the repair could not settle the host.
const (
	repairAlreadyRanEvidence    = "; its repair already ran earlier in this doctor run and did not settle it"
	repairAlreadyRanRemediation = "see the first failing check that shares this repair for its output"
)

func remoteSudoPasswordRequiredCheck(ctx context.Context, r remote.Runner, user, host string, opt Options) Result {
	command := sudoPasswordRequiredCommand(user)
	out, err := r.Run(ctx, user, host, command)
	if err == nil {
		return pass("remote", SudoPasswordCheckName, out)
	}
	if !passwordlessSudoEvidence(out, err) {
		result := fail("remote", SudoPasswordCheckName, err.Error(), SudoPasswordAuthRemediation)
		// Authentication classification must survive presentation-text changes.
		result.Code = SudoPasswordAuthFailureCode
		return result
	}
	return fixPasswordlessSudo(ctx, r, user, host, passwordlessSudoSummary(out, err), opt)
}

func fixPasswordlessSudo(ctx context.Context, r remote.Runner, user, host, evidence string, opt Options) Result {
	if !opt.Fix {
		return fail("remote", SudoPasswordCheckName, evidence, "run serverpro server doctor --fix to require sudo password")
	}
	if lifecycle.ValidateAdminPassword(opt.SudoPassword) != nil {
		return fail("remote", SudoPasswordCheckName, evidence+"; invalid sudo password", "supply a valid sudo password")
	}
	if fixErr := runRemoteWithInput(ctx, r, user, host, sudoPasswordFixCommand(user), sudoPasswordFixInput(opt.SudoPassword)); fixErr != nil {
		return fail("remote", SudoPasswordCheckName, evidence+"; fix failed: "+fixErr.Error(), "inspect remote command")
	}
	out, err := r.Run(ctx, user, host, sudoPasswordRequiredCommand(user))
	if err != nil {
		return fail("remote", SudoPasswordCheckName, err.Error(), "fix applied but check still failed")
	}
	return pass("remote", SudoPasswordCheckName, "fixed: "+out)
}

// remoteAdminPasswordHashCheck flags admin passwords still stored with
// SHA-crypt (older serverpro releases put a $6$ hash in cloud-init user data)
// or not set at all. The fix re-sets the password with plain chpasswd so the
// host hashes it with its default scheme. recheck forces a live read when an
// earlier fix in this run already re-set the password, because the batch
// baseline is stale then.
func remoteAdminPasswordHashCheck(ctx context.Context, r remote.Runner, user, host string, opt Options, recheck bool) Result {
	command := adminPasswordHashCommand(user)
	out, err := r.Run(ctx, user, host, command)
	if recheck {
		out, err = r.Run(ctx, user, host, command)
	}
	if err == nil {
		return pass("remote", AdminPasswordHashCheckName, strings.TrimSpace(out))
	}
	evidence := remoteFailureEvidence(out, err)
	if !opt.Fix {
		return fail("remote", AdminPasswordHashCheckName, evidence, "run serverpro server doctor --fix to re-set the admin password with the host's default hash")
	}
	if lifecycle.ValidateAdminPassword(opt.SudoPassword) != nil {
		return fail("remote", AdminPasswordHashCheckName, evidence+"; invalid sudo password", "supply a valid sudo password")
	}
	if fixErr := runRemoteWithInput(ctx, r, user, host, sudoPasswordFixCommand(user), sudoPasswordFixInput(opt.SudoPassword)); fixErr != nil {
		return fail("remote", AdminPasswordHashCheckName, evidence+"; fix failed: "+fixErr.Error(), "inspect remote command")
	}
	out, err = r.Run(ctx, user, host, command)
	if err != nil {
		return fail("remote", AdminPasswordHashCheckName, remoteFailureEvidence(out, err), "fix applied but check still failed")
	}
	return pass("remote", AdminPasswordHashCheckName, "fixed: "+strings.TrimSpace(out))
}

func passwordlessSudoEvidence(out string, err error) bool {
	return passwordlessSudoSummary(out, err) != ""
}

func passwordlessSudoSummary(out string, err error) string {
	if strings.Contains(out, "NOPASSWD") || (err != nil && strings.Contains(err.Error(), "NOPASSWD")) {
		return "admin sudo permits NOPASSWD:ALL"
	}
	return ""
}

func runRemoteWithInput(ctx context.Context, r remote.Runner, user, host, script, input string) error {
	inputRunner, ok := r.(remote.InputRunner)
	if !ok {
		return errors.New("remote runner does not support protected stdin input")
	}
	_, err := inputRunner.RunWithInput(ctx, user, host, script, input)
	return err
}

// remoteFixableCheck carries each command's own output into failure evidence
// because a batch error only names an index and exit status. Every caller's
// read and fix commands print host state or diagnostics, never credentials;
// a check whose output could hold a secret must not use this path.
func remoteFixableCheck(ctx context.Context, r remote.Runner, user, host, name, command, fixCommand string, opt Options) Result {
	if opt.refreshBaseline {
		// The batch baseline predates an earlier repair in this run.
		discardBaseline(r, command)
	}
	out, err := r.Run(ctx, user, host, command)
	if err == nil {
		return pass("remote", name, summarizeRemoteEvidence(name, out))
	}
	evidence := remoteFailureEvidence(out, err)
	if fixCommand == "" {
		return fail("remote", name, evidence, "inspect remote command")
	}
	if !opt.Fix {
		return fail("remote", name, evidence, "run serverpro server doctor --fix")
	}
	if opt.fixRan != nil && opt.fixRan(fixCommand) {
		return fail("remote", name, evidence+repairAlreadyRanEvidence, repairAlreadyRanRemediation)
	}
	if fixOut, fixErr := r.Run(ctx, user, host, fixCommand); fixErr != nil {
		return fail("remote", name, evidence+"; fix failed: "+remoteFailureEvidence(fixOut, fixErr), "inspect remote command")
	}
	out, err = r.Run(ctx, user, host, command)
	if err != nil {
		return fail("remote", name, remoteFailureEvidence(out, err), "fix applied but check still failed")
	}
	return pass("remote", name, "fixed: "+summarizeRemoteEvidence(name, out))
}

func remoteSSHDSettingCheckWithOptions(ctx context.Context, r remote.Runner, user, host, name, keyword string, opt Options) Result {
	value, ok := sshdHardeningExpectations[keyword]
	if !ok {
		return fail("remote", name, "missing sshd hardening expectation: "+keyword, "define expected sshd setting")
	}
	return remoteFixableCheck(ctx, r, user, host, name, sshdSettingValueCommand(keyword, value), sshdSettingsFixCommand(), opt)
}
