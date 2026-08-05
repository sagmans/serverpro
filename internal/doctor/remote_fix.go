package doctor

import (
	"context"
	"errors"
	"strings"

	"github.com/assagman/serverpro/internal/passwordhash"
	"github.com/assagman/serverpro/internal/remote"
)

func remoteCheck(ctx context.Context, r remote.Runner, user, host, name, command string) Result {
	return remoteFixableCheck(ctx, r, user, host, name, command, "", Options{})
}

func remoteSudoPasswordRequiredCheck(ctx context.Context, r remote.Runner, user, host string, opt Options) Result {
	command := sudoPasswordRequiredCommand(user)
	out, err := r.Run(ctx, user, host, command)
	if err == nil {
		return pass("remote", SudoPasswordCheckName, trim(out))
	}
	if !passwordlessSudoEvidence(out, err) {
		return fail("remote", SudoPasswordCheckName, err.Error(), SudoPasswordAuthRemediation)
	}
	return fixPasswordlessSudo(ctx, r, user, host, passwordlessSudoSummary(out, err), opt)
}

func fixPasswordlessSudo(ctx context.Context, r remote.Runner, user, host, evidence string, opt Options) Result {
	if !opt.Fix {
		return fail("remote", SudoPasswordCheckName, evidence, "run serverpro server doctor --fix to require sudo password")
	}
	if !passwordhash.ValidSHA512(opt.SudoPasswordHash) {
		return fail("remote", SudoPasswordCheckName, evidence+"; invalid sudo password hash", "supply a valid sudo password hash")
	}
	if opt.SudoPassword == "" || strings.ContainsAny(opt.SudoPassword, "\r\n") {
		return fail("remote", SudoPasswordCheckName, evidence+"; invalid sudo password", "supply a valid sudo password")
	}
	if fixErr := runRemoteWithInput(ctx, r, user, host, sudoPasswordFixCommand(user), sudoPasswordFixInput(opt.SudoPasswordHash, opt.SudoPassword)); fixErr != nil {
		return fail("remote", SudoPasswordCheckName, evidence+"; fix failed: "+fixErr.Error(), "inspect remote command")
	}
	out, err := r.Run(ctx, user, host, sudoPasswordRequiredCommand(user))
	if err != nil {
		return fail("remote", SudoPasswordCheckName, err.Error(), "fix applied but check still failed")
	}
	return pass("remote", SudoPasswordCheckName, "fixed: "+trim(out))
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

func remoteFixableCheck(ctx context.Context, r remote.Runner, user, host, name, command, fixCommand string, opt Options) Result {
	out, err := r.Run(ctx, user, host, command)
	if err == nil {
		return pass("remote", name, summarizeRemoteEvidence(name, out))
	}
	if fixCommand == "" {
		return fail("remote", name, err.Error(), "inspect remote command")
	}
	if !opt.Fix {
		return fail("remote", name, err.Error(), "run serverpro server doctor --fix")
	}
	if _, fixErr := r.Run(ctx, user, host, fixCommand); fixErr != nil {
		return fail("remote", name, err.Error()+"; fix failed: "+fixErr.Error(), "inspect remote command")
	}
	out, err = r.Run(ctx, user, host, command)
	if err != nil {
		return fail("remote", name, err.Error(), "fix applied but check still failed")
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

func remoteSSHDChallengeResponseCheckWithOptions(ctx context.Context, r remote.Runner, user, host string, opt Options) Result {
	return remoteFixableCheck(ctx, r, user, host, "sshd challenge-response auth", sshdChallengeResponseCommand(), sshdSettingsFixCommand(), opt)
}
