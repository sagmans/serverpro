package doctor

func pass(scope, name, evidence string) Result {
	return Result{Name: name, Scope: scope, Status: Pass, Evidence: trim(evidence)}
}

func warn(scope, name, evidence string) Result {
	return Result{Name: name, Scope: scope, Status: Warn, Evidence: trim(evidence)}
}

func skip(scope, name, evidence string) Result {
	return Result{Name: name, Scope: scope, Status: Skip, Evidence: trim(evidence)}
}

func fail(scope, name, evidence, fix string) Result {
	return Result{Name: name, Scope: scope, Status: Fail, Evidence: trim(evidence), Remediation: fix}
}

func trim(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}
