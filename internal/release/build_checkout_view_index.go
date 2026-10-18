// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// buildViewIndexDependency walks only the framing of captured index bytes to
// locate a split-index dependency. It does not validate or rewrite checksum,
// entries, cache flags, bitmap contents or unknown extension bodies. Private
// native Git remains the validity and cleanliness authority after byte copying.
func buildViewIndexDependency(index *os.File, hashBytes int) (string, error) {
	invalid := errors.New("captured index dependency framing is invalid")
	if hashBytes != 20 && hashBytes != 32 {
		return "", invalid
	}
	stat, err := index.Stat()
	if err != nil {
		return "", err
	}
	end := stat.Size() - int64(hashBytes)
	if end < 12 {
		return "", invalid
	}
	var header [12]byte
	if _, err := index.ReadAt(header[:], 0); err != nil {
		return "", invalid
	}
	if string(header[:4]) != "DIRC" {
		return "", invalid
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if version < 2 || version > 4 {
		return "", invalid
	}
	count := binary.BigEndian.Uint32(header[8:12])
	position := int64(12)
	for i := uint32(0); i < count; i++ {
		start := position
		if int64(42+hashBytes) > end-position {
			return "", invalid
		}
		var flags [2]byte
		if _, err := index.ReadAt(flags[:], position+int64(40+hashBytes)); err != nil {
			return "", invalid
		}
		position += int64(42 + hashBytes)
		if binary.BigEndian.Uint16(flags[:])&0x4000 != 0 {
			position += 2
		}
		if position > end {
			return "", invalid
		}
		if version == 4 {
			// Native v4 prefix encoding is variable-length. Its decoded pathname is
			// irrelevant to dependency framing; consume without interpreting it.
			for {
				if position >= end {
					return "", invalid
				}
				var c [1]byte
				if _, err := index.ReadAt(c[:], position); err != nil {
					return "", invalid
				}
				position++
				if c[0]&0x80 == 0 {
					break
				}
			}
		}
		r := bufio.NewReaderSize(io.NewSectionReader(index, position, end-position), 16<<10)
		for {
			fragment, e := r.ReadSlice(0)
			position += int64(len(fragment))
			if e == nil {
				break
			}
			if e != bufio.ErrBufferFull {
				return "", invalid
			}
		}
		if version != 4 {
			position = start + ((position-start+7)/8)*8
		}
		if position > end {
			return "", invalid
		}
	}
	dependency := ""
	for position < end {
		if end-position < 8 {
			return "", invalid
		}
		var ext [8]byte
		if _, err := index.ReadAt(ext[:], position); err != nil {
			return "", invalid
		}
		size := int64(binary.BigEndian.Uint32(ext[4:]))
		position += 8
		if size > end-position {
			return "", invalid
		}
		if string(ext[:4]) == "link" {
			if size < int64(hashBytes) {
				return "", invalid
			}
			oid := make([]byte, hashBytes)
			if _, err := index.ReadAt(oid, position); err != nil {
				return "", invalid
			}
			dependency = ""
			if !bytes.Equal(oid, make([]byte, hashBytes)) {
				dependency = "sharedindex." + hex.EncodeToString(oid)
			}
		}
		position += size
	}
	return dependency, nil
}
