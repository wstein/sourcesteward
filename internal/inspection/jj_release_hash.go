// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Hash streams a regular committed lock/tool-list blob into SHA-256. The fixed
// read inherits the session's operation pin, sandbox, deadline, bounded stderr
// and final state resampling. No content is retained or returned; there is no
// artifact-size or buffered-output limit on this distinct committed-source read.
func (r *JJReleaseReader) Hash(path string) (string, error) {
	if !r.active || r.entries[path] != "file" {
		return "", errors.New("native release hash path is not an active regular blob")
	}
	hash := sha256.New()
	code, e := r.session.executeSink(r.path, r.session.bin, releaseFileArgs(r.op, r.commit, path), hash)
	if e != nil {
		return "", jjReleaseReadError(e)
	}
	if code != 0 {
		return "", errors.New("cannot hash native release source blob")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
