// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
)

func gitHashBlob(ctx context.Context, root, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, root, "cat-file", "blob", id)
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return "", e
	}
	if e = cmd.Start(); e != nil {
		return "", e
	}
	h := sha256.New()
	_, readErr := io.Copy(h, pipe)
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if readErr != nil {
		return "", readErr
	}
	if waitErr != nil {
		return "", errGitFailed
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
