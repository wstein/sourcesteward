// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

type TidyBranch struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}

// TidyBranchAdvertisement is a complete sampled inventory. Absence has meaning
// only after successful exhaustion, and never promises future server absence.
type TidyBranchAdvertisement struct {
	Repository  TidyRepositoryIdentity `json:"repository"`
	Branches    []TidyBranch           `json:"branches"`
	Fingerprint string                 `json:"fingerprint"`
}

func (s *TidySession) readBranches(ctx context.Context) (TidyBranchAdvertisement, error) {
	if err := s.revalidateRepository(ctx); err != nil {
		return TidyBranchAdvertisement{}, err
	}
	result := TidyBranchAdvertisement{Repository: s.repository, Branches: []TidyBranch{}}
	seen := map[string]bool{}
	lastPage := 0
	for page := 1; page <= maxPages; page++ {
		endpoint := fmt.Sprintf("repos/%s/%s/branches?per_page=100&page=%d", s.selected.Owner, s.selected.Name, page)
		raw, header, err := s.get(ctx, endpoint)
		if err != nil {
			return TidyBranchAdvertisement{}, err
		}
		var rows []struct {
			Name   *string `json:"name"`
			Commit *struct {
				SHA *string `json:"sha"`
			} `json:"commit"`
		}
		if decodeTidyJSON(raw, &rows) != nil || rows == nil || len(rows) > 100 {
			return TidyBranchAdvertisement{}, errors.New("branch advertisement is incomplete or invalid")
		}
		for _, row := range rows {
			if row.Name == nil || !tidyBranchName(*row.Name) || row.Commit == nil || row.Commit.SHA == nil || !fullSHA(*row.Commit.SHA) || seen[*row.Name] {
				return TidyBranchAdvertisement{}, errors.New("branch advertisement has malformed or duplicate rows")
			}
			seen[*row.Name] = true
			result.Branches = append(result.Branches, TidyBranch{Name: *row.Name, OID: *row.Commit.SHA})
			if len(seen) > maxRecords {
				return TidyBranchAdvertisement{}, ErrTidyLimit
			}
		}
		terminal, err := tidyPagination(header, endpoint, page, len(rows), &lastPage)
		if err != nil {
			return TidyBranchAdvertisement{}, err
		}
		if terminal {
			sort.Slice(result.Branches, func(i, j int) bool { return result.Branches[i].Name < result.Branches[j].Name })
			normalized, err := json.Marshal(struct {
				Repository TidyRepositoryIdentity `json:"repository"`
				Branches   []TidyBranch           `json:"branches"`
			}{result.Repository, result.Branches})
			if err != nil || len(normalized) > maxBytes {
				return TidyBranchAdvertisement{}, ErrTidyLimit
			}
			sum := sha256.Sum256(normalized)
			result.Fingerprint = hex.EncodeToString(sum[:])
			if err = tidyResultBound(result); err != nil {
				return TidyBranchAdvertisement{}, err
			}
			return result, nil
		}
	}
	return TidyBranchAdvertisement{}, ErrTidyLimit
}
func (s *TidySession) ReadBranches(parent context.Context) (TidyBranchAdvertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return TidyBranchAdvertisement{}, err
	}
	return s.readBranches(ctx)
}
func (s *TidySession) RevalidateBranches(parent context.Context, expected TidyBranchAdvertisement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return err
	}
	if expected.Repository != s.repository {
		return ErrTidyChanged
	}
	current, err := s.readBranches(ctx)
	if err != nil {
		return err
	}
	// Compare the complete typed collection as well as its digest. A caller cannot
	// remove a row while retaining an earlier digest as absence authority.
	if !reflect.DeepEqual(current, expected) {
		return ErrTidyChanged
	}
	return nil
}
