// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

// JSON strings replace invalid UTF-8. Private native path/environment transport
// uses JSON's byte-slice/base64 representation instead; no pathname is decoded
// through Unicode or display escaping before filesystem/native consumption.
type buildViewPath string

func (p buildViewPath) MarshalJSON() ([]byte, error) { return json.Marshal([]byte(p)) }
func (p *buildViewPath) UnmarshalJSON(data []byte) error {
	var raw []byte
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = buildViewPath(raw)
	return nil
}

type buildViewIdentity struct {
	Dev, Ino       uint64
	UID, GID, Mode uint32
}
type buildViewMetadataSnapshot struct {
	Path                           buildViewPath
	Missing, RawSymlink, HasDigest bool
	Identity                       buildViewIdentity
	Size, MtimeSec, CtimeSec       int64
	MtimeNano, CtimeNano           int
	Link                           buildViewPath
	Digest                         [32]byte
}

func snapshotBuildViewMetadata(path string, b *buildViewBinding) buildViewMetadataSnapshot {
	s := buildViewMetadataSnapshot{Path: buildViewPath(path), Missing: b.info == nil, RawSymlink: b.rawSymlink, Link: buildViewPath(b.linkTarget), HasDigest: b.copied, Digest: b.digest}
	if b.info != nil {
		m, c := b.info.ModTime(), buildViewChangeStamp(b.info)
		s.Identity = buildViewFileIdentity(b.info)
		s.Size = b.info.Size()
		s.MtimeSec, s.MtimeNano = m.Unix(), m.Nanosecond()
		s.CtimeSec, s.CtimeNano = c.Unix(), c.Nanosecond()
	}
	return s
}
func (s buildViewMetadataSnapshot) verify(ctx context.Context) error {
	b, err := captureBuildViewMetadata(ctx, string(s.Path), nil)
	if err != nil {
		return errors.New("native child metadata binding unavailable")
	}
	defer b.Close()
	if s.Missing != (b.info == nil) {
		return errors.New("native child metadata presence changed")
	}
	if s.Missing {
		return b.validate()
	}
	if buildViewFileIdentity(b.info) != s.Identity || b.rawSymlink != s.RawSymlink || buildViewPath(b.linkTarget) != s.Link {
		return errors.New("native child metadata identity changed")
	}
	if !b.info.IsDir() {
		m, c := b.info.ModTime(), buildViewChangeStamp(b.info)
		if b.info.Size() != s.Size || m.Unix() != s.MtimeSec || m.Nanosecond() != s.MtimeNano || c.Unix() != s.CtimeSec || c.Nanosecond() != s.CtimeNano {
			return errors.New("native child metadata modification changed")
		}
	}
	if s.HasDigest {
		if err = b.capture(io.Discard); err != nil || b.digest != s.Digest {
			return errors.New("native child metadata content changed")
		}
	}
	return b.validate()
}
