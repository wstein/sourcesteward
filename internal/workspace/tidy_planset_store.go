// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// planSetKind is the record directory of the stored plan sets of the Spec 041 wizard. It sits
// beside the tidy v3 plan and operation directories of the same private state directory and
// holds only the manifest; the sub-plans stay ordinary v3 plans.
const planSetKind = "tidy-plan-sets"

// ErrPlanSetNotFound means the state directory holds no plan set with that identity.
var ErrPlanSetNotFound = errors.New("tidy plan set not found")

// WriteTidyPlanSet stores one plan set manifest under its content-addressed identity. The
// manifest is an opaque JSON object here: its content, identity and checks belong to
// internal/tidywizard/planset, this only supplies the private store (mode 0700, current owner,
// real directories, exclusive 0600 records, strict JSON, bounded size). Storing the same bytes
// again succeeds; a different record under an existing identity is refused.
func WriteTidyPlanSet(stateDir, id string, record []byte) error {
	var probe map[string]json.RawMessage
	if err := decodeStrict(record, &probe); err != nil || probe == nil {
		return failure("invalid-state", "A plan set record must be one strict JSON object.")
	}
	compact := new(bytes.Buffer)
	if err := json.Compact(compact, record); err != nil {
		return failure("invalid-state", "A plan set record must be one strict JSON object.")
	}
	if !v3CanonicalPath(stateDir) {
		return failure("invalid-options", "A canonical private state directory is required.")
	}
	s, err := openStoreKinds(stateDir, true, []string{planSetKind})
	if err != nil {
		return err
	}
	defer s.Close()
	err = s.write(planSetKind, id, json.RawMessage(compact.Bytes()), true)
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	var existing json.RawMessage
	if err = s.read(planSetKind, id, &existing); err != nil {
		return err
	}
	if !bytes.Equal(existing, compact.Bytes()) {
		return failure("plan-collision", "A different plan set already claims this plan identity.")
	}
	return nil
}

// ReadTidyPlanSet returns the stored manifest bytes of a plan set, or ErrPlanSetNotFound when the
// state directory holds none with that identity. It never creates anything.
func ReadTidyPlanSet(stateDir, id string) ([]byte, error) {
	if !v3CanonicalPath(stateDir) {
		return nil, failure("invalid-options", "A canonical private state directory is required.")
	}
	if _, err := os.Lstat(filepath.Join(stateDir, planSetKind)); errors.Is(err, os.ErrNotExist) {
		if _, serr := os.Stat(stateDir); serr != nil {
			return nil, serr
		}
		s, oerr := openStoreKinds(stateDir, false, nil) // refuses an unsafe directory before reporting absence
		if oerr != nil {
			return nil, oerr
		}
		s.Close()
		return nil, ErrPlanSetNotFound
	}
	s, err := openStoreKinds(stateDir, false, []string{planSetKind})
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var raw json.RawMessage
	if err = s.read(planSetKind, id, &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrPlanSetNotFound
		}
		return nil, err
	}
	return raw, nil
}
