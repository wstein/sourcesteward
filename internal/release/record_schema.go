// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	sourcesteward "github.com/wstein/sourcesteward"
)

// MaxReleaseDocumentBytes bounds record, manifest and OCI result reads.
const MaxReleaseDocumentBytes = 1024 * 1024

// ParseReleaseDocument validates the closed schema, decodes a typed model and
// compares the exact bytes with its canonical re-encoding. Invalid input never
// returns a usable model. Semantic record validation needs trusted context and
// is performed separately by ValidateRecord.
func ParseReleaseDocument(raw []byte) (any, []Finding) {
	fail := func(code, message string) (any, []Finding) { return nil, []Finding{errorFinding(code, "", message)} }
	if len(raw) > MaxReleaseDocumentBytes {
		return fail("release-limit", "release document exceeds 1 MiB")
	}
	if !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		return fail("record-invalid", "expected UTF-8 without BOM")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	doc, err := decodeJSONValue(dec, "", 0)
	if err != nil {
		return fail("record-invalid", err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail("record-invalid", "trailing JSON data")
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return fail("record-invalid", "expected document object")
	}
	var model any
	var schemaBytes []byte
	switch m["kind"] {
	case "release-record":
		model = &ReleaseRecord{}
		schemaBytes = sourcesteward.ReleaseRecordSchema
	case "build-manifest":
		model = &BuildManifest{}
		schemaBytes = sourcesteward.ReleaseBuildManifestSchema
	case "oci-build-result":
		model = &OCIResult{}
		schemaBytes = sourcesteward.ReleaseOCIResultSchema
	default:
		return fail("record-invalid", "unknown release document kind")
	}
	findings := validateRecordSchema(doc, schemaBytes)
	if len(findings) > 0 {
		return nil, findings
	}
	if err := json.Unmarshal(raw, model); err != nil {
		return fail("record-invalid", err.Error())
	}
	canonical, err := EncodeReleaseDocument(model)
	if err != nil {
		return fail("record-invalid", err.Error())
	}
	if !bytes.Equal(canonical, raw) {
		return fail("record-not-canonical", "release document differs from canonical encoding")
	}
	return model, nil
}

// Reuse the contract schema interpreter. The asset union is discriminated by
// distinct constant kind values, so oneOf is equivalent to anyOf here. Nullable
// types are expanded into ordinary type alternatives; the manifest's only
// external reference is resolved from the embedded record schema, never online.
func validateRecordSchema(doc any, raw []byte) []Finding {
	var root, record map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		panic(err)
	}
	if err := json.Unmarshal(sourcesteward.ReleaseRecordSchema, &record); err != nil {
		panic(err)
	}
	root["$defs"] = record["$defs"]
	var normalize func(any)
	normalize = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if ref, ok := n["$ref"].(string); ok && ref == "https://wstein.github.io/sourcesteward/schemas/v1/release-record.schema.json#/$defs/asset" {
				n["$ref"] = "#/$defs/asset"
			}
			if one, ok := n["oneOf"]; ok {
				n["anyOf"] = one
				delete(n, "oneOf")
			}
			if types, ok := n["type"].([]any); ok {
				options := make([]any, len(types))
				for i, t := range types {
					options[i] = map[string]any{"type": t}
				}
				n["anyOf"] = options
				delete(n, "type")
			}
			for _, child := range n {
				normalize(child)
			}
		case []any:
			for _, child := range n {
				normalize(child)
			}
		}
	}
	normalize(root)
	w := &schemaWalker{root: root, seen: map[string]bool{}}
	w.check(root, doc, "")
	for i := range w.out {
		w.out[i].Code = "record-invalid"
		w.out[i].Message = fmt.Sprintf("release document: %s", w.out[i].Message)
	}
	sortFindings(w.out)
	return w.out
}
