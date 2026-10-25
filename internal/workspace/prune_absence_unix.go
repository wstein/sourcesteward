// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

// The whole ancestor chain is held open until the last descriptor-relative
// absence and correspondence checks. No mkdir, unlock or Git command is used.
func observePruneAbsence(ctx context.Context, destination string, parent PathIdentity) (pruneAbsenceWitness, error) {
	var witness pruneAbsenceWitness
	if err := ctx.Err(); err != nil {
		return witness, err
	}
	if !validText(destination) || !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || destination == "/" || parent.Path != filepath.Dir(destination) {
		return witness, failure("prune-path-refused", "Missing checkout path must be exact, canonical and bound to its recorded parent.")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return witness, failure("prune-parent-unavailable", "Trusted filesystem root is unavailable.")
	}
	fds := []int{fd}
	defer func() {
		for i := len(fds) - 1; i >= 0; i-- {
			_ = unix.Close(fds[i])
		}
	}()
	witness = pruneAbsenceWitness{Destination: destination, Parent: parent, Ancestors: []pruneDirectoryWitness{}}
	path := "/"
	root, err := pruneDirectoryFact(fd, path)
	if err != nil {
		return witness, err
	}
	witness.Ancestors = append(witness.Ancestors, root)
	for _, part := range strings.Split(strings.TrimPrefix(parent.Path, "/"), "/") {
		if part == "" {
			continue
		}
		if err = ctx.Err(); err != nil {
			return witness, err
		}
		next, err := unix.Openat(fds[len(fds)-1], part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return witness, failure("prune-parent-unavailable", "A checkout ancestor is absent, aliased, unreadable or unavailable.")
		}
		fds = append(fds, next)
		path = filepath.Join(path, part)
		fact, err := pruneDirectoryFact(next, path)
		if err != nil {
			return witness, err
		}
		witness.Ancestors = append(witness.Ancestors, fact)
	}
	if witness.Ancestors[len(witness.Ancestors)-1].Identity != parent {
		return witness, failure("prune-parent-changed", "Checkout parent differs from its original creation identity.")
	}
	missing := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var st unix.Stat_t
		err := unix.Fstatat(fds[len(fds)-1], filepath.Base(destination), &st, unix.AT_SYMLINK_NOFOLLOW)
		return pruneMissingLeafError(err)
	}
	if err = missing(); err != nil {
		return witness, err
	}
	pruneObservationStep(ctx, "absence-first-missing", destination, nil)
	for i, opened := range fds {
		if err = ctx.Err(); err != nil {
			return witness, err
		}
		fresh, err := pruneDirectoryFact(opened, witness.Ancestors[i].Identity.Path)
		if err != nil {
			return witness, err
		}
		if fresh != witness.Ancestors[i] {
			return witness, failure("prune-absence-changed", "Checkout ancestor or mount changed during absence observation.")
		}
		if i > 0 {
			var st unix.Stat_t
			if err = unix.Fstatat(fds[i-1], filepath.Base(fresh.Identity.Path), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil || uint64(st.Dev) != fresh.Identity.Device || uint64(st.Ino) != fresh.Identity.Inode || st.Mode&unix.S_IFMT != unix.S_IFDIR {
				return witness, failure("prune-absence-changed", "Checkout ancestor pathname no longer identifies its pinned directory.")
			}
		}
	}
	pruneObservationStep(ctx, "absence-before-final-missing", destination, nil)
	if err = missing(); err != nil {
		return witness, err
	}
	return witness, nil
}
func pruneDirectoryFact(fd int, path string) (pruneDirectoryWitness, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0444 == 0 || st.Mode&0111 == 0 {
		return pruneDirectoryWitness{}, failure("prune-parent-unavailable", "Checkout ancestor must be an accessible real directory.")
	}
	mount, err := pruneMountIdentity(fd)
	if err != nil {
		return pruneDirectoryWitness{}, failure("prune-mount-unavailable", "Native mount identity is unavailable; absence is not proven.")
	}
	return pruneDirectoryWitness{Identity: PathIdentity{Path: path, Device: uint64(st.Dev), Inode: uint64(st.Ino)}, UID: uint64(st.Uid), GID: uint64(st.Gid), Mode: uint32(st.Mode), Mount: mount}, nil
}
func revalidatePruneAbsence(ctx context.Context, witness pruneAbsenceWitness) error {
	fresh, err := observePruneAbsence(ctx, witness.Destination, witness.Parent)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(fresh, witness) {
		return failure("prune-absence-changed", "Checkout ancestor or mount evidence changed; retain the registration.")
	}
	return nil
}

func pruneMissingLeafError(err error) error {
	if err == unix.ENOENT {
		return nil
	}
	if err == nil {
		return failure("prune-destination-present", "Registered checkout pathname is occupied; retain its registration.")
	}
	return failure("prune-absence-unavailable", "Checkout absence was not proven by an exact ENOENT observation.")
}
