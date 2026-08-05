package cli

import (
	"fmt"
	"io"
	"time"
)

type progressPhase string

const (
	progressPhasePreflight   progressPhase = "preflight"
	progressPhaseProvision   progressPhase = "provision"
	progressPhaseDoctor      progressPhase = "doctor"
	progressPhaseBootstrap   progressPhase = "bootstrap"
	progressPhaseGitDeploy   progressPhase = "git-deploy"
	progressPhaseGitIdentity progressPhase = "git-identity"
	progressInitialAttempt                 = 1
	progressEventFormat                    = "progress phase=%s elapsed=%s attempt=%d\n"
)

type commandProgress struct {
	writer  io.Writer
	started time.Time
	now     func() time.Time
}

func (a *app) newCommandProgress() *commandProgress {
	now := a.now
	if now == nil {
		now = time.Now
	}
	writer := a.stderr
	if writer == nil {
		// JSON stdout is a machine contract; absent explicit stderr means silent
		// progress rather than contaminating the JSON document.
		writer = io.Discard
	}
	return &commandProgress{writer: writer, started: now(), now: now}
}

func (p *commandProgress) emit(phase progressPhase) error {
	elapsed := p.now().Sub(p.started).Round(time.Millisecond)
	if elapsed < 0 {
		elapsed = 0
	}
	_, err := fmt.Fprintf(p.writer, progressEventFormat, phase, elapsed, progressInitialAttempt)
	return err
}
