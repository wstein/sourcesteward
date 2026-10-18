// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Native config output is semantic data, never a diagnostic. Keep full streams
// on private disk and refer to records by offsets, including arbitrarily long
// subsection names and values. The paired --name-only stream disambiguates keys
// containing LF from the LF separating a key and its value.
type buildViewField struct{ offset, length int64 }
type buildViewFrames struct {
	file   *os.File
	reader *bufio.Reader
	offset int64
}

func newBuildViewFrames(f *os.File) *buildViewFrames {
	return &buildViewFrames{file: f, reader: bufio.NewReader(io.NewSectionReader(f, 0, 1<<63-1))}
}
func (r *buildViewFrames) next() (buildViewField, error) {
	f := buildViewField{offset: r.offset}
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			if err == io.EOF && f.length != 0 {
				return f, io.ErrUnexpectedEOF
			}
			return f, err
		}
		r.offset++
		if b == 0 {
			return f, nil
		}
		f.length++
	}
}
func (f buildViewField) reader(file *os.File) *io.SectionReader {
	return io.NewSectionReader(file, f.offset, f.length)
}
func equalBuildViewFields(a *os.File, x buildViewField, b *os.File, y buildViewField) bool {
	if x.length != y.length {
		return false
	}
	ar, br := x.reader(a), y.reader(b)
	ab, bb := make([]byte, 16384), make([]byte, 16384)
	for left := x.length; left > 0; {
		n := int64(len(ab))
		if left < n {
			n = left
		}
		if _, err := io.ReadFull(ar, ab[:n]); err != nil {
			return false
		}
		if _, err := io.ReadFull(br, bb[:n]); err != nil {
			return false
		}
		if !bytes.Equal(ab[:n], bb[:n]) {
			return false
		}
		left -= n
	}
	return true
}
func buildViewFieldIs(f *os.File, x buildViewField, s string) bool {
	if x.length != int64(len(s)) {
		return false
	}
	b := make([]byte, len(s))
	_, err := f.ReadAt(b, x.offset)
	return err == nil && string(b) == s
}

func renderBuildViewConfig(names, values *os.File, local, worktree io.Writer) error {
	n, v := newBuildViewFrames(names), newBuildViewFrames(values)
	invalid := errors.New("native configuration captures disagree or are malformed")
	for {
		scope, err := n.next()
		if err == io.EOF {
			_, err = v.next()
			if err == io.EOF {
				return nil
			}
			return invalid
		}
		if err != nil {
			return invalid
		}
		origin, err := n.next()
		if err != nil {
			return invalid
		}
		key, err := n.next()
		if err != nil {
			return invalid
		}
		vs, err := v.next()
		if err != nil || !equalBuildViewFields(names, scope, values, vs) {
			return invalid
		}
		vo, err := v.next()
		if err != nil || !equalBuildViewFields(names, origin, values, vo) {
			return invalid
		}
		record, err := v.next()
		if err != nil || record.length < key.length {
			return invalid
		}
		prefix := buildViewField{record.offset, key.length}
		if !equalBuildViewFields(names, key, values, prefix) {
			return invalid
		}
		hasValue := record.length > key.length
		value := buildViewField{offset: record.offset + key.length, length: record.length - key.length}
		if hasValue {
			var sep [1]byte
			if _, err := values.ReadAt(sep[:], value.offset); err != nil || sep[0] != '\n' {
				return invalid
			}
			value.offset++
			value.length--
		}
		var out io.Writer
		switch {
		case buildViewFieldIs(names, scope, "local"):
			out = local
		case buildViewFieldIs(names, scope, "worktree"):
			out = worktree
		case buildViewFieldIs(names, scope, "command"):
			continue
		default:
			return invalid
		}
		// Locate section and variable without retaining a possibly large subsection.
		first, last := int64(-1), int64(-1)
		r := bufio.NewReader(key.reader(names))
		for i := int64(0); i < key.length; i++ {
			b, e := r.ReadByte()
			if e != nil {
				return invalid
			}
			if b == '.' {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first <= 0 || last == key.length-1 {
			return invalid
		}
		section := buildViewField{key.offset, first}
		variable := buildViewField{key.offset + last + 1, key.length - last - 1}
		if buildViewFieldIs(names, section, "include") && buildViewFieldIs(names, variable, "path") {
			continue
		}
		if buildViewFieldIs(names, section, "includeif") && buildViewFieldIs(names, variable, "path") {
			continue
		}
		// Git config keys are normalized by native LIST; no locale-dependent match.
		if buildViewFieldIs(names, section, "filter") && (buildViewFieldIs(names, variable, "clean") || buildViewFieldIs(names, variable, "process")) && hasValue && value.length != 0 {
			return errors.New("configured checkout conversion helper requires explicit project execution")
		}
		if err := writeBuildViewConfigRecord(out, names, section, variable, first, last, key, values, value, hasValue); err != nil {
			return err
		}
	}
}
func writeBuildViewConfigRecord(out io.Writer, names *os.File, section, variable buildViewField, first, last int64, key buildViewField, values *os.File, value buildViewField, has bool) error {
	var failure error
	write := func(s string) {
		if failure == nil {
			_, failure = io.WriteString(out, s)
		}
	}
	copyField := func(file *os.File, f buildViewField) {
		if failure == nil {
			_, failure = io.Copy(out, f.reader(file))
		}
	}
	write("[")
	copyField(names, section)
	if first != last {
		write(" \"")
		if failure == nil {
			failure = escapeBuildViewConfig(out, buildViewField{key.offset + first + 1, last - first - 1}.reader(names), false)
		}
		write("\"")
	}
	write("]\n")
	copyField(names, variable)
	if has {
		write(" = \"")
		if failure == nil {
			failure = escapeBuildViewConfig(out, value.reader(values), true)
		}
		write("\"")
	}
	write("\n")
	return failure
}
func escapeBuildViewConfig(out io.Writer, in io.Reader, value bool) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	for {
		b, err := r.ReadByte()
		if err == io.EOF {
			return w.Flush()
		}
		if err != nil {
			return err
		}
		var text string
		switch b {
		case '\\':
			text = `\\`
		case '"':
			text = `\"`
		case '\n':
			if value {
				text = `\n`
			}
		case '\t':
			if value {
				text = `\t`
			}
		case '\b':
			if value {
				text = `\b`
			}
		}
		if text != "" {
			_, err = w.WriteString(text)
		} else {
			err = w.WriteByte(b)
		}
		if err != nil {
			return err
		}
	}
}

// Each edge is observed through the parent descriptor used to acquire its child.
// Retain all parents, including symlink origins, until the job revalidates/closes.
// Do not reacquire a recursive path from its original string after a rename.
type buildViewEdge struct {
	parent  *os.Root
	name    string
	info    os.FileInfo
	link    string
	missing bool
}
type buildViewBinding struct {
	parents    []*os.Root
	edges      []buildViewEdge
	file       *os.File
	info       os.FileInfo
	digest     [32]byte
	copied     bool
	rawSymlink bool
	linkTarget string
}

func (b *buildViewBinding) Close() error {
	var err error
	if b.file != nil {
		err = b.file.Close()
	}
	for i := len(b.parents) - 1; i >= 0; i-- {
		err = errors.Join(err, b.parents[i].Close())
	}
	b.file = nil
	b.parents = nil
	return err
}
func sameBuildViewIdentity(a, b os.FileInfo) bool { return os.SameFile(a, b) && a.Mode() == b.Mode() }
func sameBuildViewFile(a, b os.FileInfo) bool {
	return sameBuildViewIdentity(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime() && buildViewChangeStamp(a) == buildViewChangeStamp(b)
}
func captureBuildViewPath(ctx context.Context, path string, step func(string)) (*buildViewBinding, error) {
	return captureBuildViewPathMode(ctx, path, step, false)
}
func captureBuildViewMetadata(ctx context.Context, path string, step func(string)) (*buildViewBinding, error) {
	return captureBuildViewPathMode(ctx, path, step, true)
}
func captureBuildViewPathMode(ctx context.Context, path string, step func(string), rawFinal bool) (_ *buildViewBinding, retErr error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("checkout metadata path is not absolute")
	}
	b := &buildViewBinding{}
	defer func() {
		if retErr != nil {
			_ = b.Close()
		}
	}()
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	b.parents = append(b.parents, root)
	stack := []*os.Root{root}
	type component struct {
		name    string
		release os.FileInfo
	}
	components := func(text string) []component {
		var out []component
		for _, name := range strings.Split(text, string(filepath.Separator)) {
			out = append(out, component{name: name})
		}
		return out
	}
	pending := components(path)
	var active []os.FileInfo
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := stack[len(stack)-1]
		if len(pending) == 0 {
			f, e := current.Open(".")
			if e != nil {
				return nil, e
			}
			b.file = f
			b.info, e = f.Stat()
			if e != nil {
				return nil, e
			}
			return b, nil
		}
		part := pending[0]
		pending = pending[1:]
		if part.release != nil {
			active = active[:len(active)-1]
			continue
		}
		name := part.name
		if name == "" || name == "." {
			continue
		}
		if name == ".." {
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		info, e := current.Lstat(name)
		hasRemaining := false
		for _, part := range pending {
			if part.release == nil && part.name != "" && part.name != "." {
				hasRemaining = true
				break
			}
		}
		if errors.Is(e, os.ErrNotExist) && !hasRemaining {
			b.edges = append(b.edges, buildViewEdge{parent: current, name: name, missing: true})
			return b, nil
		}
		if e != nil {
			return nil, e
		}
		edge := buildViewEdge{parent: current, name: name, info: info}
		if info.Mode()&os.ModeSymlink != 0 {
			for _, prior := range active {
				if os.SameFile(prior, info) {
					return nil, errors.New("checkout metadata symlink cycle")
				}
			}
			active = append(active, info)
			link, e := current.Readlink(name)
			if e != nil {
				return nil, e
			}
			again, e := current.Lstat(name)
			if e != nil || !sameBuildViewFile(info, again) {
				return nil, errors.New("checkout metadata symlink changed")
			}
			edge.link = link
			b.edges = append(b.edges, edge)
			if rawFinal && !hasRemaining {
				b.rawSymlink = true
				b.linkTarget = link
				b.info = info
				return b, nil
			}
			if filepath.IsAbs(link) {
				stack = stack[:1]
			}
			pending = append(append(components(link), component{release: info}), pending...)
			continue
		}
		b.edges = append(b.edges, edge)
		if info.IsDir() {
			next, e := current.OpenRoot(name)
			if e != nil {
				return nil, e
			}
			b.parents = append(b.parents, next)
			actual, e := next.Stat(".")
			if e != nil || !sameBuildViewIdentity(info, actual) {
				return nil, errors.New("checkout metadata directory changed")
			}
			stack = append(stack, next)
			if step != nil {
				step("opened-directory:" + next.Name())
			}
			continue
		}
		if hasRemaining {
			return nil, errors.New("checkout metadata component is not a directory")
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("checkout metadata input is not a regular file")
		}
		if step != nil {
			step("before-file")
		}
		f, e := openBuildViewFile(current, name)
		if e != nil {
			return nil, e
		}
		b.file = f
		actual, e := f.Stat()
		if e != nil || !sameBuildViewFile(info, actual) {
			return nil, errors.New("checkout metadata file changed")
		}
		b.info = actual
		return b, nil
	}
}
func (b *buildViewBinding) copyTo(out *os.File) error {
	if b.file == nil || !b.info.Mode().IsRegular() {
		return errors.New("checkout metadata file absent or not regular")
	}
	hash := sha256.New()
	if _, err := io.CopyBuffer(io.MultiWriter(out, hash), io.NewSectionReader(b.file, 0, b.info.Size()), make([]byte, 32<<10)); err != nil {
		return err
	}
	after, err := b.file.Stat()
	if err != nil || !sameBuildViewFile(b.info, after) {
		return errors.New("checkout metadata changed while captured")
	}
	copy(b.digest[:], hash.Sum(nil))
	b.copied = true
	return nil
}
func (b *buildViewBinding) validate() error {
	for _, edge := range b.edges {
		actual, err := edge.parent.Lstat(edge.name)
		if edge.missing {
			if !errors.Is(err, os.ErrNotExist) {
				return errors.New("absent checkout metadata changed")
			}
			continue
		}
		if err != nil || !sameBuildViewIdentity(edge.info, actual) {
			return errors.New("checkout metadata edge changed")
		}
		if edge.info.Mode()&os.ModeSymlink != 0 {
			link, e := edge.parent.Readlink(edge.name)
			if e != nil || link != edge.link || !sameBuildViewFile(edge.info, actual) {
				return errors.New("checkout metadata symlink changed")
			}
		}
	}
	if b.file == nil {
		return nil
	}
	actual, err := b.file.Stat()
	if err != nil {
		return err
	}
	if b.info.Mode().IsRegular() {
		if !sameBuildViewFile(b.info, actual) {
			return errors.New("checkout metadata modification changed")
		}
		if b.copied {
			hash := sha256.New()
			if _, err := io.CopyBuffer(hash, io.NewSectionReader(b.file, 0, b.info.Size()), make([]byte, 32<<10)); err != nil {
				return err
			}
			if !bytes.Equal(hash.Sum(nil), b.digest[:]) {
				return errors.New("checkout metadata content changed")
			}
			after, err := b.file.Stat()
			if err != nil || !sameBuildViewFile(b.info, after) {
				return errors.New("checkout metadata changed during validation")
			}
		}
	} else if !sameBuildViewIdentity(b.info, actual) {
		return errors.New("checkout metadata root changed")
	}
	return nil
}

// Native comparison never inherits project credentials, loader/startup hooks or
// arbitrary GIT_* injection. Preserve native pathname/locale semantics explicitly.
func buildViewEnvironment(inherited []string, realGit, private string) []string {
	var env []string
	for _, entry := range inherited {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "HOME", "XDG_CONFIG_HOME", "XDG_CONFIG_DIRS", "XDG_DATA_HOME", "XDG_DATA_DIRS", "XDG_STATE_HOME", "XDG_CACHE_HOME", "LANG", "LANGUAGE", "TZ", "SUDO_UID":
			env = append(env, entry)
		default:
			if strings.HasPrefix(key, "LC_") {
				env = append(env, entry)
			}
		}
	}
	return append(env, "PATH="+filepath.Dir(realGit)+":/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR="+private, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ATTR_NOSYSTEM=1", "GIT_LITERAL_PATHSPECS=1")
}

// The lease is released by the whole comparison job only after actual cleanup;
// native command completion alone never makes another capture job admissible.
var buildViewLease = make(chan struct{}, 1)

func acquireBuildViewLease(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case buildViewLease <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-buildViewLease
			return nil, err
		}
		return func() { <-buildViewLease }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type buildViewCommand struct {
	Executable, Dir string
	Args, Env       []string
	Stdin           *os.File
}
type buildViewGroup struct {
	members        int
	leaderTerminal bool
}
