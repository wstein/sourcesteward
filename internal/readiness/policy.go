// SPDX-License-Identifier: EUPL-1.2
// Package readiness parses bounded immutable inputs without reading external state.
// Shape validity alone establishes no authority, authentication or eligibility.
package readiness

type Repository struct {
	Host     string `json:"host"`
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	BaseRef  string `json:"base_ref"`
}
type Model struct {
	ID     string `json:"id"`
	Effort string `json:"effort"`
}
type Tier struct {
	Name   string  `json:"name"`
	Rank   int64   `json:"rank"`
	Models []Model `json:"models"`
}
type PathTier struct {
	Prefix string `json:"prefix"`
	Tier   string `json:"tier"`
}
type ReviewPolicy struct {
	Context           string     `json:"context"`
	StatusCreatorIDs  []int64    `json:"status_creator_ids"`
	ReportAuthorIDs   []int64    `json:"report_author_ids"`
	Tiers             []Tier     `json:"tiers"`
	DefaultTier       string     `json:"default_tier"`
	PathTiers         []PathTier `json:"path_tiers"`
	MaxRounds         int64      `json:"max_rounds"`
	Independence      string     `json:"independence"`
	AllowLowFollowups bool       `json:"allow_low_followups"`
}
type CIRequirement struct {
	Context    string  `json:"context"`
	AppID      *int64  `json:"app_id"`
	CreatorIDs []int64 `json:"creator_ids"`
}
type MergePolicy struct {
	Method        string `json:"method"`
	RequireStrict bool   `json:"require_strict"`
}
type PolicyDeployment struct {
	Profile        string `json:"profile"`
	PublisherAppID *int64 `json:"publisher_app_id"`
}

// PolicyData is a caller-owned snapshot; changing it cannot alter a parsed Policy.
type PolicyData struct {
	SchemaVersion int64            `json:"schema_version"`
	PolicyID      string           `json:"policy_id"`
	Version       int64            `json:"version"`
	Repository    Repository       `json:"repository"`
	Review        ReviewPolicy     `json:"review"`
	CI            []CIRequirement  `json:"ci"`
	Merge         MergePolicy      `json:"merge"`
	Deployment    PolicyDeployment `json:"deployment"`
	TrustRoots    []string         `json:"trust_roots"`
}
type PolicySource struct {
	RepositoryID int64  `json:"repository_id"`
	FullName     string `json:"full_name"`
	Commit       string `json:"commit"`
	SHA256       string `json:"sha256"`
}
type Checker struct {
	SourceSHA      string `json:"source_sha"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}
type LockDeployment struct {
	Profile        string  `json:"profile"`
	Environment    *string `json:"environment"`
	PublisherAppID *int64  `json:"publisher_app_id"`
}
type Transition struct {
	RepositoryID             int64  `json:"repository_id"`
	Number                   int64  `json:"number"`
	HeadSHA                  string `json:"head_sha"`
	OldGeneration            int64  `json:"old_generation"`
	OldPolicySHA256          string `json:"old_policy_sha256"`
	NewPolicyCommit          string `json:"new_policy_commit"`
	NewPolicySHA256          string `json:"new_policy_sha256"`
	NewCheckerSourceSHA      string `json:"new_checker_source_sha"`
	NewCheckerArtifactSHA256 string `json:"new_checker_artifact_sha256"`
}

// LockData includes the entire authority record, including explicit null transition.
type LockData struct {
	SchemaVersion int64          `json:"schema_version"`
	Repository    Repository     `json:"repository"`
	Generation    int64          `json:"generation"`
	PolicySource  PolicySource   `json:"policy_source"`
	Checker       Checker        `json:"checker"`
	Deployment    LockDeployment `json:"deployment"`
	Transition    *Transition    `json:"transition"`
}
type Policy struct{ document[PolicyData] }
type Lock struct{ document[LockData] }

func ParsePolicy(raw []byte) (Policy, error) {
	d, e := parseDocument[PolicyData](raw, 64*1024, policyShape())
	return Policy{d}, e
}
func ParseLock(raw []byte) (Lock, error) {
	d, e := parseDocument[LockData](raw, 16*1024, lockShape())
	return Lock{d}, e
}

const tokenPattern = `^[A-Za-z0-9][A-Za-z0-9._/-]*$`

func tokenShape() *shape   { return text(64, tokenPattern) }
func idShape() *shape      { return numeric(9007199254740991) }
func shaShape() *shape     { return text(40, `^[0-9a-f]{40}$`) }
func digestShape() *shape  { return text(64, `^[0-9a-f]{64}$`) }
func profileShape() *shape { return choice("none", "required-workflow", "protected-source", "app") }
func repositoryShape() *shape {
	return obj(map[string]*shape{"host": text(253, ""), "id": idShape(), "full_name": text(201, ""), "base_ref": text(200, "")})
}
func modelShape() *shape { return obj(map[string]*shape{"id": tokenShape(), "effort": tokenShape()}) }
func policyShape() *shape {
	return obj(map[string]*shape{
		"schema_version": numeric(1), "policy_id": tokenShape(), "version": idShape(), "repository": repositoryShape(),
		"review": obj(map[string]*shape{
			"context": text(100, ""), "status_creator_ids": array(idShape(), 1, 20), "report_author_ids": array(idShape(), 1, 20),
			"tiers":        array(obj(map[string]*shape{"name": tokenShape(), "rank": numeric(20), "models": array(modelShape(), 1, 20)}), 1, 20),
			"default_tier": tokenShape(), "path_tiers": array(obj(map[string]*shape{"prefix": text(1024, ""), "tier": tokenShape()}), 0, 100),
			"max_rounds": numeric(20), "independence": choice("declared-execution"), "allow_low_followups": boolean(),
		}),
		"ci":          array(obj(map[string]*shape{"context": text(100, ""), "app_id": nullable(idShape()), "creator_ids": array(idShape(), 0, 20)}), 0, 100),
		"merge":       obj(map[string]*shape{"method": choice("rebase"), "require_strict": boolean()}),
		"deployment":  obj(map[string]*shape{"profile": profileShape(), "publisher_app_id": nullable(idShape())}),
		"trust_roots": array(text(1024, ""), 1, 100),
	})
}
func lockShape() *shape {
	return obj(map[string]*shape{
		"schema_version": numeric(1), "repository": repositoryShape(), "generation": idShape(),
		"policy_source": obj(map[string]*shape{"repository_id": idShape(), "full_name": text(201, ""), "commit": shaShape(), "sha256": digestShape()}),
		"checker":       obj(map[string]*shape{"source_sha": shaShape(), "artifact_sha256": digestShape()}),
		"deployment":    obj(map[string]*shape{"profile": profileShape(), "environment": nullable(text(64, `^[A-Za-z0-9_-]+$`)), "publisher_app_id": nullable(idShape())}),
		"transition": nullable(obj(map[string]*shape{
			"repository_id": idShape(), "number": idShape(), "head_sha": shaShape(), "old_generation": idShape(), "old_policy_sha256": digestShape(),
			"new_policy_commit": shaShape(), "new_policy_sha256": digestShape(), "new_checker_source_sha": shaShape(), "new_checker_artifact_sha256": digestShape(),
		})),
	})
}
