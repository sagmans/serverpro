package doctor

// Counts preserves successful checks without repeating their evidence in summaries.
type Counts struct {
	Pass  int `json:"pass"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
	Skip  int `json:"skip"`
	Total int `json:"total"`
}

// Summary keeps actionable diagnostics beside the location of the complete report.
type Summary struct {
	Namespace  string   `json:"namespace"`
	Server     string   `json:"server"`
	Status     Status   `json:"status"`
	Counts     Counts   `json:"counts"`
	Results    []Result `json:"results"`
	ReportPath string   `json:"report_path"`
}

// Summary excludes successful detail without changing the original report.
func (r Report) Summary(namespace, server, reportPath string) Summary {
	r = r.boundedEvidence()
	s := Summary{Namespace: namespace, Server: server, Status: Pass, Results: []Result{}, ReportPath: reportPath}
	for _, result := range r.Results {
		s.Counts.Total++
		switch result.Status {
		case Pass:
			s.Counts.Pass++
		case Warn:
			s.Counts.Warn++
			if s.Status != Fail {
				s.Status = Warn
			}
		case Fail:
			s.Counts.Fail++
			s.Status = Fail
		case Skip:
			s.Counts.Skip++
		}
		if result.Status != Pass {
			s.Results = append(s.Results, result)
		}
	}
	return s
}
