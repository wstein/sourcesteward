// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"reflect"

	sourcesteward "github.com/wstein/sourcesteward"
)

// RecordContext supplies independently resolved evidence. Contract must be the
// normalized contract read from built commit C, Repository the hosting-resolved
// owner/name, and Checksums the parsed, bounded checksum file (name to digest).
// SignerWorkflowFile overrides only the workflow path for explicit bootstrap use.
// This function does no I/O and never treats record fields as trusted context.
type RecordContext struct {
	Contract           *Contract
	Repository         string
	SignerWorkflowFile string
	Checksums          map[string]string
}

// ValidateRecord applies structural and semantic rules from contracts/record.md.
// Attestation verification and evidence acquisition belong to later consumers;
// passing this check alone does not authenticate artifacts or the built commit.
func ValidateRecord(record ReleaseRecord, context RecordContext) []Finding {
	add := func(pointer, message string) Finding { return errorFinding("record-invalid", pointer, message) }
	node, err := recordNode(reflect.ValueOf(record))
	if err != nil {
		return []Finding{add("", err.Error())}
	}
	out := validateRecordSchema(node, sourcesteward.ReleaseRecordSchema)
	if len(out) > 0 {
		return out
	}
	canonical, err := EncodeReleaseDocument(record)
	if err != nil {
		return []Finding{add("", err.Error())}
	}
	if len(canonical) > MaxReleaseDocumentBytes {
		return []Finding{add("", "release record exceeds 1 MiB")}
	}
	c := context.Contract
	if c == nil {
		return []Finding{add("", "trusted contract is required")}
	}
	scheme, err := CompileVersionScheme(c.Version)
	if err != nil || !scheme.Valid(record.Version) {
		out = append(out, add("/version", "version does not match the contract scheme"))
	} else {
		tag, err := scheme.Tag(record.Version)
		if err != nil || tag != record.Tag {
			out = append(out, add("/tag", "tag does not equal the tag derived from the version"))
		}
	}
	if context.Repository == "" || record.Repository != context.Repository {
		out = append(out, add("/repository", "repository differs from hosting resolution"))
	}
	if record.Project != c.Project.Name {
		out = append(out, add("/project", "project differs from the contract"))
	}
	if record.Product != c.Project.Product {
		out = append(out, add("/product", "product differs from the contract"))
	}
	workflow := c.CI.ReleaseWorkflow
	if context.SignerWorkflowFile != "" {
		workflow = context.SignerWorkflowFile
	}
	if record.Attestation.SignerWorkflow != context.Repository+"/"+workflow {
		out = append(out, add("/attestation/signer_workflow", "signer workflow differs from the trusted workflow"))
	}
	names := map[string]bool{}
	for i, asset := range record.Assets {
		p := fmt.Sprintf("/assets/%d", i)
		if a := asset.File; a != nil {
			if names[a.Name] {
				out = append(out, add(p+"/name", "file asset names must be unique"))
			}
			names[a.Name] = true
			if hash, ok := context.Checksums[a.Name]; !ok || hash != a.SHA256 {
				out = append(out, add(p+"/sha256", "file digest is missing or different in checksums"))
			}
			if problem := placeholderProblem(a.URLTemplate, placeholdersURL); problem != "" {
				out = append(out, add(p+"/url_template", problem))
			}
			if a.URLTemplate != c.Outputs.URLTemplate {
				out = append(out, add(p+"/url_template", "URL template differs from the contract"))
			}
		} else {
			a := asset.Image
			if len(a.Tags) != 1 || a.Tags[0] != record.Version {
				out = append(out, add(p+"/tags", "image tags must equal exactly [version]"))
			}
		}
	}
	sortFindings(out)
	return out
}
