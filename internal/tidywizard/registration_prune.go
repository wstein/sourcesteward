// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import "github.com/wstein/sourcesteward/internal/tidywizard/owner"

// RegistrationPruneVerdict is the separate contract-4 row verdict vocabulary.
// It does not extend the frozen legacy candidate contract.
type RegistrationPruneVerdict string

const (
	RegistrationPruneSafe    RegistrationPruneVerdict = "safe"
	RegistrationPruneRetain  RegistrationPruneVerdict = "retain"
	RegistrationPruneBlocked RegistrationPruneVerdict = "blocked"
	RegistrationPruneUnknown RegistrationPruneVerdict = "unknown"
)

// RegistrationPrune describes one exact administrative registration, with no
// integration proof, branch leg, raw metadata or private recovery payload.
// A safe row is sampled evidence only; persistence and apply must revalidate it.
type RegistrationPrune struct {
	ID             string                   `json:"id"`
	Kind           string                   `json:"kind"`
	Repo           string                   `json:"repo"`
	Name           string                   `json:"name"`
	GitDir         string                   `json:"gitdir"`
	Destination    string                   `json:"destination"`
	Owner          string                   `json:"owner"`
	OwnerSource    owner.Source             `json:"owner_source"`
	Scope          owner.Scope              `json:"scope"`
	Task           string                   `json:"task"`
	Verdict        RegistrationPruneVerdict `json:"verdict"`
	Reason         string                   `json:"reason"`
	Proof          string                   `json:"proof"`
	Effects        []string                 `json:"effects"`
	EvidenceDigest string                   `json:"evidence_digest"`
	Limitations    []string                 `json:"limitations"`
	// Risk fields are optional overview annotations, never admission evidence.
	Risk        string   `json:"risk,omitempty"`
	RiskReasons []string `json:"risk_reasons,omitempty"`
}
