// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxArtifactBytes int64 = 2 << 30
const maxArtifactFiles = 64

type ArtifactObservation struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Outcome string `json:"outcome"`
}

func assetName(s string) bool { return ValidRepoPath(s) && !strings.Contains(s, "/") }
func parseChecksums(raw []byte) (map[string]string, error) {
	if len(raw) > 65536 {
		return nil, errReleaseLimit
	}
	m := map[string]string{}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return nil, fmt.Errorf("checksum lines require a trailing newline")
	}
	lines := strings.Split(string(raw[:len(raw)-1]), "\n")
	if len(lines) > 65 {
		return nil, errReleaseLimit
	}
	for _, s := range lines {
		if len(s) < 67 || s[64] != ' ' || (s[65] != ' ' && s[65] != '*') || !assetName(s[66:]) {
			return nil, fmt.Errorf("invalid checksum line")
		}
		h := s[:64]
		b, e := hex.DecodeString(h)
		if e != nil || len(b) != 32 || strings.ToLower(h) != h {
			return nil, fmt.Errorf("invalid SHA-256")
		}
		name := s[66:]
		if _, ok := m[name]; ok {
			return nil, fmt.Errorf("duplicate checksum name")
		}
		m[name] = h
	}
	return m, nil
}
func hashArtifact(ctx context.Context, path string) (FileRef, error) {
	f, e := openRegular(path)
	if e != nil {
		return FileRef{}, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil {
		return FileRef{}, e
	}
	if !before.Mode().IsRegular() {
		return FileRef{}, fmt.Errorf("non-regular artifact")
	}
	if before.Size() > maxArtifactBytes {
		return FileRef{}, errReleaseLimit
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var size int64
	for {
		if e = ctx.Err(); e != nil {
			return FileRef{}, e
		}
		n, re := f.Read(buf)
		size += int64(n)
		if size > maxArtifactBytes {
			return FileRef{}, errReleaseLimit
		}
		h.Write(buf[:n])
		if re == io.EOF {
			break
		}
		if re != nil {
			return FileRef{}, re
		}
	}
	after, e := f.Stat()
	if e != nil {
		return FileRef{}, e
	}
	if before.Size() != size || after.Size() != size || !before.ModTime().Equal(after.ModTime()) {
		return FileRef{}, errNotStable
	}
	return FileRef{filepath.Base(path), fmt.Sprintf("%x", h.Sum(nil)), size}, nil
}
func inspectArtifacts(dir string, want []FileRef) ([]ArtifactObservation, []Gate, error) {
	return inspectArtifactsContext(context.Background(), dir, want)
}
func inspectArtifactsContext(ctx context.Context, dir string, want []FileRef) ([]ArtifactObservation, []Gate, error) {
	observations := []ArtifactObservation{}
	gates := []Gate{}
	expected := map[string]FileRef{}
	for _, f := range want {
		if !assetName(f.Name) {
			return nil, nil, fmt.Errorf("invalid expected artifact name")
		}
		if _, ok := expected[f.Name]; ok {
			return nil, nil, fmt.Errorf("duplicate expected artifact")
		}
		expected[f.Name] = f
	}
	d, e := os.Open(dir)
	if e != nil {
		return nil, nil, e
	}
	defer d.Close()
	entries, e := d.ReadDir(maxArtifactFiles + 3)
	if e != nil && e != io.EOF {
		return nil, nil, e
	}
	if len(entries) > maxArtifactFiles+2 {
		return nil, nil, errReleaseLimit
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	missing, unexpected, mismatch := []string{}, []string{}, []string{}
	seen := map[string]bool{}
	for _, entry := range entries {
		if e = ctx.Err(); e != nil {
			return nil, nil, e
		}
		name := entry.Name()
		want, ok := expected[name]
		if !ok || !entry.Type().IsRegular() {
			unexpected = append(unexpected, Escape(name))
			continue
		}
		seen[name] = true
		actual, e := hashArtifact(ctx, filepath.Join(dir, name))
		if errors.Is(e, errReleaseLimit) || errors.Is(e, context.Canceled) {
			return nil, nil, e
		}
		if e != nil {
			unexpected = append(unexpected, Escape(name))
			continue
		}
		outcome := "pass"
		if actual.Size != want.Size || actual.SHA256 != want.SHA256 {
			outcome = "fail"
			mismatch = append(mismatch, Escape(name))
		}
		observations = append(observations, ArtifactObservation{name, actual.Size, actual.SHA256, outcome})
	}
	for name := range expected {
		if !seen[name] {
			missing = append(missing, Escape(name))
		}
	}
	sort.Strings(missing)
	for _, v := range []struct {
		code  string
		names []string
	}{{"artifact-missing", missing}, {"artifact-unexpected", unexpected}, {"artifact-mismatch", mismatch}} {
		outcome := "pass"
		if len(v.names) > 0 {
			outcome = "fail"
		}
		gates = append(gates, gate(v.code, outcome, "", strings.Join(v.names, ", ")))
	}
	return observations, gates, nil
}

// readArtifactBytes is used only for bounded metadata documents, never large assets.
func readArtifactBytes(path string, limit int) ([]byte, error) {
	f, e := openRegular(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("non-regular artifact")
	}
	data, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(data) > limit {
		return nil, errReleaseLimit
	}
	return data, e
}
