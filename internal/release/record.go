// SPDX-License-Identifier: EUPL-1.2
package release

import "encoding/json"

// ReleaseRecord models the version 1 release contract.
type ReleaseRecord struct {
	SchemaVersion   int               `json:"schema_version"`
	Kind            string            `json:"kind"`
	Project         string            `json:"project"`
	Product         string            `json:"product,omitempty"`
	Version         string            `json:"version"`
	Tag             string            `json:"tag"`
	Commit          string            `json:"commit"`
	Repository      string            `json:"repository"`
	SourceDateEpoch *int64            `json:"source_date_epoch"`
	Build           RecordBuild       `json:"build"`
	Assets          []RecordAsset     `json:"assets"`
	Checksums       FileRef           `json:"checksums"`
	Manifest        FileRef           `json:"manifest"`
	Attestation     RecordAttestation `json:"attestation"`
}

// BuildManifest models the version 1 release contract.
type BuildManifest struct {
	SchemaVersion   int             `json:"schema_version"`
	Kind            string          `json:"kind"`
	Project         string          `json:"project"`
	Product         string          `json:"product,omitempty"`
	Version         string          `json:"version"`
	Commit          string          `json:"commit"`
	SourceDateEpoch *int64          `json:"source_date_epoch"`
	ConfigSHA256    string          `json:"config_sha256"`
	STWVersion      string          `json:"stw_version"`
	Repository      string          `json:"repository"`
	Run             RecordRun       `json:"run"`
	Builds          []ManifestBuild `json:"builds"`
	Assets          []RecordAsset   `json:"assets"`
}

// OCIResult models the version 1 release contract.
type OCIResult struct {
	SchemaVersion int             `json:"schema_version"`
	Kind          string          `json:"kind"`
	IndexDigest   string          `json:"index_digest"`
	Platforms     []ImagePlatform `json:"platforms"`
}

// RecordBuild models the version 1 release contract.
type RecordBuild struct {
	Workflow   string `json:"workflow"`
	RunID      int64  `json:"run_id"`
	RunAttempt int64  `json:"run_attempt"`
	STWVersion string `json:"stw_version"`
}

// RecordRun models the version 1 release contract.
type RecordRun struct {
	Workflow   string `json:"workflow"`
	RunID      int64  `json:"run_id"`
	RunAttempt int64  `json:"run_attempt"`
}

// RecordAttestation models the version 1 release contract.
type RecordAttestation struct {
	Provider       string `json:"provider"`
	SignerWorkflow string `json:"signer_workflow"`
	PredicateType  string `json:"predicate_type"`
}

// FileRef models the version 1 release contract.
type FileRef struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// PathSHA models the version 1 release contract.
type PathSHA struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ManifestBuild models the version 1 release contract.
type ManifestBuild struct {
	ID           string    `json:"id"`
	Network      string    `json:"network"`
	NetworkHosts []string  `json:"network_hosts"`
	LockFiles    []PathSHA `json:"lock_files"`
}

// ImagePlatform models the version 1 release contract.
type ImagePlatform struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Variant        string `json:"variant,omitempty"`
	ManifestDigest string `json:"manifest_digest"`
}

// FileAsset models the version 1 release contract.
type FileAsset struct {
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Role        string `json:"role"`
	Group       string `json:"group,omitempty"`
	Build       string `json:"build"`
	Name        string `json:"name"`
	URLTemplate string `json:"url_template"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

// OCIImageAsset models the version 1 release contract.
type OCIImageAsset struct {
	Kind        string          `json:"kind"`
	Target      string          `json:"target"`
	Role        string          `json:"role"`
	Group       string          `json:"group,omitempty"`
	Build       string          `json:"build"`
	Repository  string          `json:"repository"`
	IndexDigest string          `json:"index_digest"`
	Platforms   []ImagePlatform `json:"platforms"`
	Tags        []string        `json:"tags"`
	Lock        *PathSHA        `json:"lock"`
	ToolList    *PathSHA        `json:"tool_list"`
	SourceOffer *string         `json:"source_offer"`
}

// RecordAsset is the closed file/image union. Exactly one arm must be set.
type RecordAsset struct {
	File  *FileAsset
	Image *OCIImageAsset
}

func (a *RecordAsset) UnmarshalJSON(raw []byte) error {
	var kind struct{ Kind string }
	if err := json.Unmarshal(raw, &kind); err != nil {
		return err
	}
	if kind.Kind == "file" {
		a.File = &FileAsset{}
		return json.Unmarshal(raw, a.File)
	}
	a.Image = &OCIImageAsset{}
	return json.Unmarshal(raw, a.Image)
}
