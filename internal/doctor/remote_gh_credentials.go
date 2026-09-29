package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/remote"
)

const (
	ghTokenParityCheckName = "gh token parity"
	// Mismatched fingerprints have two possible winners and doctor cannot know
	// which copy is current, so drift is reported for a human decision only.
	ghTokenParityConvergeRemediation = "rerun serverpro server bootstrap NAME git to converge gh credentials"
	ghTokenParityDeployHint          = "run serverpro server doctor NAME --fix to deploy the stored PAT"
	// Rotation is the only remaining move once GitHub itself rejected the stored
	// PAT: deploying it again would only re-publish a credential known to be dead.
	ghTokenParityRotateRemediation = "rotate the GitHub PAT, then rerun serverpro server bootstrap NAME git"
	// An unanswered probe is evidence about transport, not about the credential, so
	// the operator gets the reachability lead instead of a credential verdict.
	ghTokenParityUnverifiedRemediation = "check remote egress to api.github.com, then rerun serverpro server doctor NAME"
	ghTokenParityRetryRemediation      = "rerun serverpro server doctor NAME --fix"
)

// ghAuthVerdict keeps a proven rejection distinct from every situation where
// GitHub never answered, because only a proved rejection may replace the remote
// credential: a toolchain or egress failure must never overwrite a working token.
type ghAuthVerdict string

const (
	ghAuthOK      ghAuthVerdict = "ok"
	ghAuthFailed  ghAuthVerdict = "failed"
	ghAuthAbsent  ghAuthVerdict = "absent"
	ghAuthUnknown ghAuthVerdict = "unknown"
)

type ghCredentialSnapshot struct {
	fingerprint string
	auth        ghAuthVerdict
}

// parseGHCredentialRead keeps the wire protocol tolerant of extra lines so an
// ssh banner can never corrupt the parity verdict, and reads a missing verdict
// line as unknown: silence must never authorise a credential write.
func parseGHCredentialRead(out string) ghCredentialSnapshot {
	snapshot := ghCredentialSnapshot{auth: ghAuthUnknown}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "sha=absent":
			snapshot.fingerprint = ""
		case strings.HasPrefix(line, "sha="):
			snapshot.fingerprint = strings.TrimPrefix(line, "sha=")
		case strings.HasPrefix(line, "auth="):
			snapshot.auth = ghAuthVerdict(strings.TrimPrefix(line, "auth="))
		}
	}
	return snapshot
}

// credentialFingerprint is deliberately short: it exists for human drift
// comparison, not for collision-resistant identification of a secret.
func credentialFingerprint(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:ghTokenFingerprintLength]
}

// ghTokenParityCheck compares the locally stored PAT with the remote gh token.
// Only a credential GitHub itself rejects is overwritten: a working remote token
// with a different fingerprint is reported because either copy could be the
// stale one, and an unverifiable remote stays unresolved instead of repaired.
func ghTokenParityCheck(ctx context.Context, r remote.Runner, user, host, readCommand string, opt Options) Result {
	out, err := r.Run(ctx, user, host, readCommand)
	if err != nil {
		return fail("remote", ghTokenParityCheckName, err.Error(), "inspect remote command")
	}
	remoteState := parseGHCredentialRead(out)
	localFingerprint := credentialFingerprint(opt.GitHubPAT)
	switch {
	case remoteState.fingerprint == "" && localFingerprint == "":
		return skip("remote", ghTokenParityCheckName, "no github PAT stored locally or in remote hosts.yml")
	case remoteState.auth == ghAuthUnknown:
		result := warn("remote", ghTokenParityCheckName, ghUnverifiedEvidence(localFingerprint, remoteState.fingerprint))
		result.Remediation = ghTokenParityUnverifiedRemediation
		return result
	case remoteState.auth == ghAuthOK && remoteState.fingerprint == localFingerprint:
		return pass("remote", ghTokenParityCheckName, "local=remote sha="+localFingerprint)
	case remoteState.auth == ghAuthOK:
		result := warn("remote", ghTokenParityCheckName, ghDriftEvidence(localFingerprint, remoteState.fingerprint))
		result.Remediation = ghTokenParityConvergeRemediation
		return result
	case localFingerprint == "":
		result := warn("remote", ghTokenParityCheckName, "GitHub rejected the remote gh token and no PAT is stored locally; remote sha="+fingerprintOrAbsent(remoteState.fingerprint))
		result.Remediation = ghTokenParityRotateRemediation
		return result
	default:
		return deployLocalGHToken(ctx, r, user, host, readCommand, localFingerprint, opt)
	}
}

func ghDriftEvidence(local, remoteFingerprint string) string {
	if local == "" {
		return "remote gh token sha=" + remoteFingerprint + "; no PAT stored locally"
	}
	return "local sha=" + local + " differs from remote sha=" + remoteFingerprint
}

// ghUnverifiedEvidence names what stayed undecided; reporting a credential
// verdict here would send the operator after the wrong repair.
func ghUnverifiedEvidence(local, remoteFingerprint string) string {
	localState := "no PAT stored locally"
	if local != "" {
		localState = "local sha=" + local
	}
	return "GitHub did not answer the credential probe; remote sha=" + fingerprintOrAbsent(remoteFingerprint) + "; " + localState
}

func fingerprintOrAbsent(fingerprint string) string {
	if fingerprint == "" {
		return "absent"
	}
	return fingerprint
}

// deployLocalGHToken reuses the bootstrap token writer so hosts.yml keeps one
// authoritative producer; GHTokenScript validates the PAT against GitHub before
// writing, so a failed deploy leaves the remote credential untouched and proves
// the local copy is dead too.
func deployLocalGHToken(ctx context.Context, r remote.Runner, user, host, readCommand, localFingerprint string, opt Options) Result {
	if !opt.Fix {
		result := warn("remote", ghTokenParityCheckName, "GitHub rejected the remote gh token; local sha="+localFingerprint+" is available")
		result.Remediation = ghTokenParityDeployHint
		return result
	}
	if strings.ContainsAny(opt.GitHubPAT, "\r\n") {
		return fail("remote", ghTokenParityCheckName, "stored PAT contains line breaks", ghTokenParityRotateRemediation)
	}
	if err := runRemoteWithInput(ctx, r, user, host, lifecycle.GHTokenScript(user), opt.GitHubPAT+"\n"); err != nil {
		return fail("remote", ghTokenParityCheckName, "deploying the stored PAT failed: "+err.Error(), ghTokenParityRotateRemediation)
	}
	recheck, err := r.Run(ctx, user, host, readCommand)
	if err != nil {
		return fail("remote", ghTokenParityCheckName, err.Error(), "inspect remote command")
	}
	recheckState := parseGHCredentialRead(recheck)
	switch {
	case recheckState.fingerprint != localFingerprint:
		return fail("remote", ghTokenParityCheckName, "deployed the stored PAT but remote hosts.yml does not hold it", ghTokenParityRetryRemediation)
	case recheckState.auth == ghAuthOK:
		return pass("remote", ghTokenParityCheckName, "fixed: deployed local sha="+localFingerprint)
	case recheckState.auth == ghAuthFailed:
		return fail("remote", ghTokenParityCheckName, "GitHub rejected the stored PAT that was just deployed", ghTokenParityRotateRemediation)
	default:
		return fail("remote", ghTokenParityCheckName, "deployed the stored PAT but GitHub did not answer the recheck", ghTokenParityRetryRemediation)
	}
}
