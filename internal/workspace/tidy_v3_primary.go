// SPDX-License-Identifier: EUPL-1.2
package workspace

import "sort"

// Preserve the complete native row, replacing only Git's metadata-directory
// spelling of the primary path with the checkout bound by its layout proof.
func projectTidyV3Primary(rows []V3Registration, id NativeIdentity, binding gitPrimaryBinding) ([]V3Registration, error) {
	bad := func() ([]V3Registration, error) {
		return nil, failure("incomplete-evidence", "A unique canonical primary registration requires authoritative, nonconflicting native checkout evidence.")
	}
	if binding.Conflicting {
		return bad()
	}
	seen := map[string]bool{}
	primary := -1
	for i, row := range rows {
		if seen[row.Path] {
			return bad()
		}
		seen[row.Path] = true
		if row.Path == id.RepositoryDir {
			primary = i
		}
	}
	if primary < 0 {
		if binding.Root != "" && !seen[binding.Root] {
			return bad()
		}
		return append([]V3Registration{}, rows...), nil
	}
	if !v3CanonicalPath(binding.Root) || binding.Root == "" {
		return bad()
	}
	if binding.Root != id.RepositoryDir && seen[binding.Root] {
		return bad()
	}
	out := append([]V3Registration{}, rows...)
	out[primary].Path = binding.Root
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
