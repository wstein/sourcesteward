// SPDX-License-Identifier: EUPL-1.2
package workspace

// RegistrationPruneV4Candidate identifies a native registration for read-only
// admission. Task is a lock hint only; it never proves ownership.
type RegistrationPruneV4Candidate struct {
	Name, GitDir, Destination, Task string
}

// RegistrationPruneV4Inventory contains only missing or inaccessible linked
// destinations. It carries no integration proof or permission to remove them.
type RegistrationPruneV4Inventory struct {
	Status     string
	Candidates []RegistrationPruneV4Candidate
	Diagnostic Diagnostic
}

// RegistrationPruneV4Observation is in-process evidence, never a JSON result or
// persisted plan. Expected can contain sensitive paths; render only the row.
type RegistrationPruneV4Observation struct {
	Status         string
	Registration   PruneV4Registration
	Expected       PruneV4Expected
	EvidenceDigest string
	Diagnostic     Diagnostic
	Limitations    []string
	admittedDigest string
}

// Complete rejects missing admission and any change to a returned witness.
// It is sampled read-only proof, never freshness or effect authorization.
func (o RegistrationPruneV4Observation) Complete() bool {
	if o.Status != "ok" || o.admittedDigest == "" || o.EvidenceDigest != o.admittedDigest {
		return false
	}
	digest, err := pruneV4OverviewDigest(o.Registration, o.Expected)
	return err == nil && digest == o.admittedDigest
}

func pruneV4OverviewDigest(reg PruneV4Registration, expected PruneV4Expected) (string, error) {
	return pruneV4Digest("stw-registration-evidence-v4", struct {
		SchemaVersion int                 `json:"schema_version"`
		Operation     string              `json:"operation"`
		Registration  PruneV4Registration `json:"registration"`
		Expected      PruneV4Expected     `json:"expected"`
	}{4, "registration-prune", reg, expected}, "")
}

func refusedPruneV4Overview(err error) RegistrationPruneV4Observation {
	r := failPruneV4(pruneV4Result(), err)
	return RegistrationPruneV4Observation{Status: "refused", Diagnostic: r.Diagnostics[0], Limitations: []string{}}
}
