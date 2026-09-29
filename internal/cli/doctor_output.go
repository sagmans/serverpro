package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/redact"
)

const doctorLogWarning = "warning: cannot save doctor report: %s\n"

func (a *app) writeDoctorReport(cfg config.Config, creds credentials.Set, report doctor.Report) error {
	redactor := redact.New(a.redactionSecrets(creds)...)
	// Hooks and provider failures must cross the same redaction boundary as normal checks.
	report = report.RedactForOutput(a.redactionSecrets(creds)...)
	path, saveErr := report.Save(cfg.Namespace, targetServer(cfg.Server))
	if saveErr != nil {
		// A storage failure must never suppress diagnostics, even when stderr also fails.
		outputErr := report.Write(a.stdout)
		stderr := a.stderr
		if stderr == nil {
			stderr = io.Discard
		}
		_, warningErr := fmt.Fprintf(stderr, doctorLogWarning, redactor.String(saveErr.Error()))
		return redactor.Error(errors.Join(outputErr, warningErr))
	}
	if a.doctorFull {
		return redactor.Error(report.Write(a.stdout))
	}
	summary := report.Summary(redactor.String(cfg.Namespace), redactor.String(targetServer(cfg.Server)), redactor.String(path))
	return redactor.Error(writeJSON(a.stdout, summary))
}
