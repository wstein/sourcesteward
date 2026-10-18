// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// validateLocalComparison binds the comparison to independently read inputs
// before export or execution. Comparison assets are never folded into a map
// until both their names and target identities have proved unique.
func validateLocalComparison(ctx context.Context, s buildState, document any) (map[string]string, string, error) {
	var project, product, commit, version, repository string
	var epoch *int64
	var assets []RecordAsset
	switch d := document.(type) {
	case *ReleaseRecord:
		project, product, commit, version, repository, epoch, assets = d.Project, d.Product, d.Commit, d.Version, d.Repository, d.SourceDateEpoch, d.Assets
	case *BuildManifest:
		project, product, commit, version, repository, epoch, assets = d.Project, d.Product, d.Commit, d.Version, d.Repository, d.SourceDateEpoch, d.Assets
		if d.ConfigSHA256 != s.configSHA || d.Run.Workflow != s.contract.CI.ReleaseWorkflow {
			return nil, "", errors.New("manifest input identity differs from the trusted contract")
		}
	default:
		return nil, "", errors.New("expected record or build manifest")
	}
	if project != s.contract.Project.Name || product != s.contract.Project.Product || commit != s.commit || version != s.version || (epoch != nil && *epoch != s.epoch) {
		return nil, "", errors.New("comparison identity differs from the trusted build inputs")
	}
	targets := map[string]Target{}
	for _, target := range s.contract.Target {
		targets[target.ID] = target
	}
	expected := map[string]string{}
	seenTargets := map[string]bool{}
	for _, asset := range assets {
		a := asset.File
		if a == nil {
			return nil, "", errors.New("comparison asset is not a file")
		}
		if _, exists := expected[a.Name]; exists || seenTargets[a.Target] {
			return nil, "", errors.New("comparison asset names and targets must be unique")
		}
		t, ok := targets[a.Target]
		if !ok || t.Kind != "file" || a.Name != path.Base(s.values(&t)["output"]) || a.Build != t.Build || a.Role != t.Role || a.Group != t.Group || a.OS != t.OS || a.Arch != t.Arch || a.URLTemplate != s.contract.Outputs.URLTemplate {
			return nil, "", fmt.Errorf("comparison target %s differs from the trusted contract", a.Target)
		}
		expected[a.Name], seenTargets[a.Target] = a.SHA256, true
	}
	if len(seenTargets) != len(targets) {
		return nil, "", errors.New("comparison asset set differs from the trusted contract")
	}
	if manifest, ok := document.(*BuildManifest); ok {
		used := map[string]bool{}
		for _, target := range s.contract.Target {
			used[target.Build] = true
		}
		expectedBuilds := []ManifestBuild{}
		for _, build := range s.contract.Build {
			if used[build.ID] {
				disclosure, err := s.manifestBuild(build)
				if err != nil {
					return nil, "", err
				}
				expectedBuilds = append(expectedBuilds, disclosure)
			}
		}
		if !reflect.DeepEqual(manifest.Builds, expectedBuilds) {
			return nil, "", errors.New("manifest build provenance differs from the trusted contract")
		}
	}
	resolved := hosting.Inspect(ctx, s.root, hosting.Options{Kind: "repo"})
	if resolved.Status != "ok" || resolved.Repository == nil {
		return nil, "", errors.New("cannot resolve trusted repository identity")
	}
	trustedRepository := resolved.Repository.Owner + "/" + resolved.Repository.Name
	if repository != trustedRepository {
		return nil, "", errors.New("comparison repository differs from hosting resolution")
	}
	if d, ok := document.(*ReleaseRecord); ok {
		if d.Build.Workflow != s.contract.CI.ReleaseWorkflow {
			return nil, "", errors.New("record workflow differs from the trusted contract")
		}
		if findings := ValidateRecord(*d, RecordContext{Contract: s.contract, Repository: trustedRepository, Checksums: expected}); len(findings) > 0 {
			return nil, "", errors.New(findings[0].Message)
		}
	}
	return expected, trustedRepository, nil
}
