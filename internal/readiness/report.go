// SPDX-License-Identifier: EUPL-1.2
package readiness

type ReportModel struct {
	ID       string `json:"id"`
	Effort   string `json:"effort"`
	Evidence string `json:"evidence"`
}
type Independence struct {
	Kind            string `json:"kind"`
	SeparateAuthor  bool   `json:"separate_author"`
	SeparateContext bool   `json:"separate_context"`
}
type Round struct {
	Number   int64    `json:"number"`
	HeadSHA  string   `json:"head_sha"`
	Verdict  string   `json:"verdict"`
	Findings []string `json:"findings"`
}
type Finding struct {
	ID       string  `json:"id"`
	Severity string  `json:"severity"`
	State    string  `json:"state"`
	Owner    *string `json:"owner"`
	Evidence *string `json:"evidence"`
}

// ReportData contains public declaration fields; parsing proves shape only.
type ReportData struct {
	SchemaVersion int64        `json:"schema_version"`
	ReportID      string       `json:"report_id"`
	RepositoryID  int64        `json:"repository_id"`
	Number        int64        `json:"number"`
	HeadSHA       string       `json:"head_sha"`
	BaseSHA       string       `json:"base_sha"`
	PolicyCommit  string       `json:"policy_commit"`
	PolicySHA256  string       `json:"policy_sha256"`
	Scope         string       `json:"scope"`
	FilesSHA256   string       `json:"files_sha256"`
	Verdict       string       `json:"verdict"`
	Reviewer      string       `json:"reviewer"`
	Model         ReportModel  `json:"model"`
	Independence  Independence `json:"independence"`
	Rounds        []Round      `json:"rounds"`
	Findings      []Finding    `json:"findings"`
	Blockers      []string     `json:"blockers"`
}
type Report struct{ document[ReportData] }

func ParseReport(raw []byte) (Report, error) {
	d, e := parseDocument[ReportData](raw, 64*1024, reportShape())
	return Report{d}, e
}

func reportShape() *shape {
	truth := &shape{kind: 'b', trueOnly: true}
	return obj(map[string]*shape{
		"schema_version": numeric(1), "report_id": tokenShape(), "repository_id": idShape(), "number": idShape(),
		"head_sha": shaShape(), "base_sha": shaShape(), "policy_commit": shaShape(), "policy_sha256": digestShape(),
		"scope": choice("full-pr"), "files_sha256": digestShape(), "verdict": choice("clear", "not-clear"), "reviewer": text(200, ""),
		"model":        obj(map[string]*shape{"id": tokenShape(), "effort": tokenShape(), "evidence": choice("host-configured")}),
		"independence": obj(map[string]*shape{"kind": choice("declared-execution"), "separate_author": truth, "separate_context": truth}),
		"rounds":       array(obj(map[string]*shape{"number": numeric(20), "head_sha": shaShape(), "verdict": choice("clear", "not-clear"), "findings": array(tokenShape(), 0, 100)}), 1, 20),
		"findings":     array(obj(map[string]*shape{"id": tokenShape(), "severity": choice("critical", "high", "medium", "low"), "state": choice("open", "resolved", "accepted-followup"), "owner": nullable(text(200, "")), "evidence": nullable(text(2048, ""))}), 0, 100),
		"blockers":     array(text(2048, ""), 0, 100),
	})
}
