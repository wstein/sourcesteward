// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The architecture rules of internal/release, enforced on the syntax tree (not on text), so
// spacing, line breaks, aliases and unusual formatting cannot hide a use:
//   - processes start only in vcs_*.go, attest.go and build.go (os/exec, os.StartProcess);
//   - syscall is imported only by openfile_*.go (and build.go for process groups), without an
//     alias, and in openfile_*.go its only uses are the selector expressions syscall.O_*;
//   - no cgo (import "C", cgo directives), no assembly or other foreign sources, no unsafe, no
//     go:linkname or plugin. The two checkout-view platform observers
//     (build_checkout_view_linux.go and build_checkout_view_darwin.go) may import syscall for the
//     selector expression syscall.Stat_t only. The exact record writer alone imports pinned x/sys/unix;
//     only direct Openat/Mkdirat calls and its explicit O_* constants are admitted.
//     Direct imports and the non-hosting dependency closure are closed to other external helpers.
var (
	processFile = regexp.MustCompile(`^(vcs_[a-z0-9_]+|attest|build)\.go$`)
	openFile    = regexp.MustCompile(`^openfile_[a-z0-9_]+\.go$`)
	// The platform observers of the private checkout view. This is an exact-name exception, not
	// a build_* wildcard: subdirectories and other names get no exemption.
	checkoutObserver = regexp.MustCompile(`^build_checkout_view_(linux|darwin)\.go$`)
	foreignFiles     = regexp.MustCompile(`(?i)\.(s|sx|c|cc|cpp|cxx|c\+\+|h|hh|hpp|hxx|h\+\+|m|f|for|ftn|f77|f90|f95|f03|f08|syso|swig|swigcxx)$`)
)

var standardOnce sync.Once
var standardImports map[string]bool
var standardImportError error

func standardImportInventory() (map[string]bool, error) {
	standardOnce.Do(func() {
		standardImports = map[string]bool{}
		raw, err := exec.Command("go", "list", "std").Output()
		standardImportError = err
		for _, path := range strings.Fields(string(raw)) {
			standardImports[path] = true
		}
	})
	return standardImports, standardImportError
}

const releaseModule = "github.com/wstein/sourcesteward"

func releaseProjectImport(path string) bool {
	return path == releaseModule || path == releaseModule+"/internal/inspection" || path == releaseModule+"/internal/hosting" || path == releaseModule+"/internal/release" || strings.HasPrefix(path, releaseModule+"/internal/release/")
}

var writerUnixConstants = map[string]bool{"O_RDONLY": true, "O_WRONLY": true, "O_CREAT": true, "O_EXCL": true, "O_NOFOLLOW": true, "O_DIRECTORY": true, "O_CLOEXEC": true, "O_NONBLOCK": true}

// releaseSourceViolations checks the files of internal/release (name -> content).
func releaseSourceViolations(files map[string]string) []string {
	var out []string
	standards, standardErr := standardImportInventory()
	if standardErr != nil {
		return []string{"cannot inspect Go standard-library metadata: " + standardErr.Error()}
	}
	add := func(name, why string) { out = append(out, name+": "+why) }
	for name, text := range files {
		if foreignFiles.MatchString(name) {
			add(name, "foreign source files (assembly, C, ...) are not allowed")
			continue
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, text, parser.ParseComments)
		if err != nil {
			add(name, "does not parse: "+err.Error())
			continue
		}
		local := map[string]string{} // import path -> local name
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			named := spec.Name != nil
			unixWriter := path == "golang.org/x/sys/unix" && name == "openfile_write_unix.go"
			if !standards[path] && !releaseProjectImport(path) && !unixWriter {
				add(name, "import outside the closed release dependency boundary: "+path)
			}
			switch {
			case path == "C":
				add(name, `import "C" (cgo) is not allowed`)
			case path == "golang.org/x/sys/unix" && name == "openfile_write_unix.go":
				if named {
					add(name, "writer unix import must have no alias, dot or blank name")
				}
			case path == "unsafe", path == "plugin", strings.HasPrefix(path, "golang.org/x/sys"):
				add(name, "import of "+path+" is not allowed")
			case path == "syscall":
				if !openFile.MatchString(name) && name != "build.go" && !checkoutObserver.MatchString(name) {
					add(name, "syscall may be imported only by openfile_*.go, build.go and the checkout view observers")
				} else if named {
					add(name, "syscall must be imported without an alias, dot or blank name")
				}
			case path == "os/exec":
				if !processFile.MatchString(name) {
					add(name, "os/exec may be imported only by vcs_*.go, attest.go and build.go")
				} else if named {
					add(name, "os/exec must be imported without an alias, dot or blank name")
				}
			case path == "os" && named && spec.Name.Name != "os" && (spec.Name.Name == "." || spec.Name.Name == "_"):
				add(name, "os must not be imported with a dot or blank name")
			}
			if named {
				local[path] = spec.Name.Name
			} else {
				local[path] = filepath.Base(path)
			}
		}
		for _, group := range file.Comments {
			for _, c := range group.List {
				if strings.HasPrefix(c.Text, "//go:linkname") || strings.HasPrefix(c.Text, "//go:cgo") {
					add(name, "the directive "+strings.Fields(c.Text)[0]+" is not allowed")
				}
			}
		}
		osName := local["os"]
		if osName == "" {
			osName = "os"
		}
		allowedWriterRefs := map[*ast.Ident]bool{}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "writeRecordExclusive" {
				allowedWriterRefs[fn.Name] = true
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "writeRecordExclusive" && len(call.Args) == 3 && call.Ellipsis == token.NoPos {
					allowedWriterRefs[id] = true
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "writeRecordExclusive" && !allowedWriterRefs[id] {
				add(name, "production may only call the private record writer directly with its three ordinary arguments")
			}
			return true
		})
		allowedSyscall := map[*ast.Ident]bool{}
		allowedUnix := map[*ast.Ident]bool{}
		directWriterCalls := map[*ast.SelectorExpr]bool{}
		if name == "openfile_write_unix.go" {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "writeRecordExclusive" {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
							directWriterCalls[sel] = true
						}
					}
					return true
				})
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if x.Name == "syscall" && openFile.MatchString(name) && strings.HasPrefix(sel.Sel.Name, "O_") {
				allowedSyscall[x] = true
			}
			if x.Name == "syscall" && checkoutObserver.MatchString(name) && sel.Sel.Name == "Stat_t" {
				allowedSyscall[x] = true
			}
			if x.Name == "unix" && name == "openfile_write_unix.go" && (writerUnixConstants[sel.Sel.Name] || (sel.Sel.Name == "Openat" || sel.Sel.Name == "Mkdirat") && directWriterCalls[sel]) {
				allowedUnix[x] = true
			}
			if x.Name == osName && sel.Sel.Name == "StartProcess" && !processFile.MatchString(name) {
				add(name, "os.StartProcess is not allowed outside vcs_*.go, attest.go and build.go")
			}
			return true
		})
		if checkoutObserver.MatchString(name) {
			imports := map[*ast.Ident]bool{}
			for _, spec := range file.Imports {
				if spec.Name != nil {
					imports[spec.Name] = true
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "syscall" && !allowedSyscall[id] && !imports[id] {
					add(name, "the checkout view observers may use only syscall.Stat_t, found "+fset.Position(id.Pos()).String())
				}
				return true
			})
		}
		if openFile.MatchString(name) {
			imports := map[*ast.Ident]bool{}
			for _, spec := range file.Imports {
				if spec.Name != nil {
					imports[spec.Name] = true
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "unix" && name == "openfile_write_unix.go" && !allowedUnix[id] && !imports[id] {
					add(name, "unix permits only exact constants and direct filesystem calls inside writeRecordExclusive")
				}
				if id, ok := n.(*ast.Ident); ok && id.Name == "syscall" && !allowedSyscall[id] && !imports[id] {
					add(name, "unapproved syscall use (only O_* constants and the dedicated writer Openat/Mkdirat calls are allowed), found "+fset.Position(id.Pos()).String())
				}
				return true
			})
		}
	}
	sort.Strings(out)
	return out
}

// readTree reads every file below root (testdata directories excepted) keyed by slash path.
func readTree(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	return files, err
}

func TestReleaseSourceArchitecture(t *testing.T) {
	files, err := readTree(".")
	if err != nil {
		t.Fatal(err)
	}
	if v := releaseSourceViolations(files); len(v) != 0 {
		t.Fatalf("architecture violations:\n%s", strings.Join(v, "\n"))
	}
}

// The walk reaches subpackages: assembly, cgo and process starts there are found on disk.
func TestReleaseSourceArchitectureWalksSubpackages(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"ok.go":               "package release\n",
		"zzsub/zz.s":          "TEXT ·f(SB),0,$0\n",
		"zzc/zz.go":           "package zzc\n\nimport \"C\"\n",
		"zzexec/deeper/zz.go": "package deeper\n\nimport \"os/exec\"\n",
		"zzhx/zz.hxx":         "x",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := readTree(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(releaseSourceViolations(files), "\n")
	for _, want := range []string{"zzsub/zz.s", "zzc/zz.go", "zzexec/deeper/zz.go", "zzhx/zz.hxx"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was not reported:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ok.go") {
		t.Errorf("false positive:\n%s", got)
	}
}

func TestReleaseSourceArchitectureCatchesTheBypasses(t *testing.T) {
	pkg := "package release\n\n"
	opener := pkg + "import (\n\t\"os\"\n\t\"syscall\"\n)\n\nvar f = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK\n"
	bad := map[string]map[string]string{
		"spaced selector":         {"openfile_unix.go": pkg + "import \"syscall\"\n\nvar _ = syscall . ForkExec\n"},
		"selector over lines":     {"openfile_unix.go": pkg + "import \"syscall\"\n\nvar _ = syscall.\n\tForkExec\n"},
		"second aliased import":   {"openfile_unix.go": pkg + "import (\n\t\"syscall\"\n\tsc \"syscall\"\n)\n\nvar _ = sc.ForkExec\nvar _ = syscall.O_NOFOLLOW\n"},
		"dot import of syscall":   {"openfile_unix.go": pkg + "import . \"syscall\"\n\nvar _ = O_NOFOLLOW\n"},
		"blank import of syscall": {"openfile_unix.go": pkg + "import _ \"syscall\"\n"},
		"syscall as a value":      {"openfile_unix.go": pkg + "import \"syscall\"\n\nvar _ = syscall.O_NOFOLLOW\nvar g = syscall.Getpid\n"},
		"syscall passed on":       {"openfile_unix.go": pkg + "import \"syscall\"\n\nfunc f(x func() int) {}\n\nfunc h() { f(syscall.Getpid) }\n"},
		"syscall elsewhere":       {"check.go": pkg + "import \"syscall\"\n\nvar _ = syscall.O_NOFOLLOW\n"},
		"syscall in a vcs file":   {"vcs_git.go": opener},
		"cgo":                     {"zz.go": pkg + "import \"C\"\n"},
		"cgo with a preamble":     {"zz.go": pkg + "/*\n#include <unistd.h>\n*/\nimport \"C\"\n"},
		"cgo directive":           {"zz.go": pkg + "//go:cgo_import_dynamic x y z\nvar _ = 1\n"},
		"assembly":                {"zz.s": "TEXT ·f(SB),0,$0\n\tRET\n"},
		"upper case assembly":     {"zz.S": "TEXT ·f(SB),0,$0\n"},
		"a C source":              {"zz.c": "int f(){return 0;}\n"},
		"unsafe":                  {"zz.go": pkg + "import _ \"unsafe\"\n"},
		"linkname":                {"zz.go": pkg + "import _ \"unsafe\"\n\n//go:linkname zzfork syscall.forkExec\nfunc zzfork()\n"},
		"linkname only":           {"zz.go": pkg + "//go:linkname zzfork os.runtime_beforeFork\nfunc zzfork()\n"},
		"os/exec elsewhere":       {"zz.go": pkg + "import \"os/exec\"\n"},
		"aliased os/exec":         {"vcs_git.go": pkg + "import x \"os/exec\"\n"},
		"aliased os StartProcess": {"zz.go": pkg + "import o \"os\"\n\nvar _, _ = o.StartProcess(\"a\", nil, nil)\n"},
		"plain os StartProcess":   {"zz.go": pkg + "import \"os\"\n\nvar _, _ = os.StartProcess(\"a\", nil, nil)\n"},
		"dot os":                  {"zz.go": pkg + "import . \"os\"\n"},
		"x/sys":                   {"zz.go": pkg + "import \"golang.org/x/sys/unix\"\n"},
		"plugin":                  {"zz.go": pkg + "import \"plugin\"\n"},
		"broken source":           {"zz.go": "package release\nfunc {"},
	}
	// Subpackages are covered: a nested directory gets no exemption from the file names.
	nested := map[string]map[string]string{
		"assembly in a subpackage":        {"zzsub/zz.s": "TEXT ·f(SB),0,$0\n"},
		"cgo in a subpackage":             {"zzc/zz.go": pkg + "import \"C\"\n"},
		"os/exec in a subpackage":         {"zzsub/zz.go": pkg + "import \"os/exec\"\n"},
		"a reviewed name in a subpackage": {"zzsub/vcs_git.go": pkg + "import \"os/exec\"\n"},
		"syscall in a subpackage opener":  {"zzsub/openfile_unix.go": pkg + "import \"syscall\"\n\nvar _ = syscall.O_NOFOLLOW\n"},
		".sx":                             {"zz.sx": "x"},
		".hxx":                            {"zz.hxx": "x"},
		".for":                            {"zz.for": "x"},
		".f03":                            {"zz.f03": "x"},
		".c++":                            {"zz.c++": "x"},
		".SX upper case":                  {"zz.SX": "x"},
	}
	for name, files := range nested {
		bad[name] = files
	}
	for name, files := range bad {
		if v := releaseSourceViolations(files); len(v) == 0 {
			t.Errorf("%s was not caught", name)
		}
	}
	good := map[string]string{
		"openfile_unix.go": opener,
		"vcs_git.go":       pkg + "import \"os/exec\"\n",
		"attest.go":        pkg + "import \"os/exec\"\n",
		"build.go":         pkg + "import (\n\t\"os\"\n\t\"os/exec\"\n\t\"syscall\"\n)\n\nvar _, _ = os.StartProcess, exec.Command\nvar _ = syscall.Kill\n",
		"check.go":         pkg + "import \"os\"\n\n// mentions syscall.ForkExec and go:linkname in a comment\nvar s = \"import C\"\nvar _ = os.Getpid\n",
		"zz_test.go":       pkg + "import \"os/exec\"\n",
		"notes.txt":        "plain text",
	}
	if v := releaseSourceViolations(good); len(v) != 0 {
		t.Errorf("false positives: %v", v)
	}
}

func TestReleaseSourceArchitecturePortableWriter(t *testing.T) {
	const writer = "openfile_write_unix.go"
	prefix := "package release\nimport \"golang.org/x/sys/unix\"\n"
	safe := prefix + "func writeRecordExclusive() { _, _ = unix.Openat(0, \"x\", unix.O_RDONLY, 0); _ = unix.Mkdirat(0, \"x\", 0755) }\n"
	if v := releaseSourceViolations(map[string]string{writer: safe}); len(v) > 0 {
		t.Fatal(v)
	}
	for _, fn := range []string{"Openat", "Mkdirat"} {
		for _, form := range []string{"var captured = unix.%s\n", "func capture() { f := unix.%s; _ = f }\n", "func returned() any { return unix.%s }\n", "func consume(any) {}\nfunc passed() { consume(unix.%s) }\n", "var table = []any{unix.%s}\n"} {
			mutant := prefix + fmt.Sprintf(form, fn)
			if v := releaseSourceViolations(map[string]string{writer: mutant}); len(v) == 0 {
				t.Fatalf("accepted %s: %s", fn, form)
			}
		}
	}
	for _, mutant := range []string{prefix + "func forward() { _, _ = unix.Openat(0, \"x\", 0, 0) }", prefix + "var _, _ = unix.Openat(0, \"x\", 0, 0)", prefix + "func writeRecordExclusive() { _, _ = (unix.Openat)(0, \"x\", 0, 0) }", prefix + "func writeRecordExclusive() { _, _ = unix.Socket(0, 0, 0) }"} {
		if len(releaseSourceViolations(map[string]string{writer: mutant})) == 0 {
			t.Fatalf("accepted %s", mutant)
		}
	}
	for _, wrong := range []string{"openfile_unix.go", "record.go", "sub/openfile_write_unix.go"} {
		if len(releaseSourceViolations(map[string]string{wrong: safe})) == 0 {
			t.Fatalf("accepted writer import in %s", wrong)
		}
	}
	for _, alias := range []string{"u", ".", "_"} {
		source := strings.Replace(prefix, "import \"", "import "+alias+" \"", 1)
		if len(releaseSourceViolations(map[string]string{writer: source})) == 0 {
			t.Fatalf("accepted alias %s", alias)
		}
	}
	for _, file := range []string{"record.go", "sub/helper.go", writer} {
		if len(releaseSourceViolations(map[string]string{file: "package release\nimport \"example.org/foreignhelper\"\n"})) == 0 {
			t.Fatalf("accepted foreign helper in %s", file)
		}
	}
}

func TestReleaseSourceArchitectureProductionCannotInjectWriterHooks(t *testing.T) {
	for _, body := range []string{"func caller() { writeRecordExclusive(\"root\",\"docs/releases/v1.json\",nil,recordWriteHooks{}) }", "var capturedWriter = writeRecordExclusive", "func caller() { _ = writeRecordExclusive }"} {
		if len(releaseSourceViolations(map[string]string{"record.go": "package release\n" + body})) == 0 {
			t.Fatalf("accepted production hook route %s", body)
		}
	}
}

func TestReleaseSourceArchitectureCheckoutObserversAreTypeOnly(t *testing.T) {
	source := "package release\nimport \"syscall\"\nvar _ *syscall.Stat_t\n"
	for _, name := range []string{"build_checkout_view_linux.go", "build_checkout_view_darwin.go"} {
		if v := releaseSourceViolations(map[string]string{name: source}); len(v) != 0 {
			t.Fatalf("%s rejected: %v", name, v)
		}
		for _, selector := range []string{"Kill", "Exec", "ForkExec", "Syscall", "RawSyscall", "O_TRUNC", "Getpgid"} {
			if len(releaseSourceViolations(map[string]string{name: source + "var _ = syscall." + selector + "\n"})) == 0 {
				t.Fatalf("%s accepted syscall.%s", name, selector)
			}
		}
		for _, alias := range []string{"s", ".", "_"} {
			if len(releaseSourceViolations(map[string]string{name: strings.Replace(source, "import \"", "import "+alias+" \"", 1)})) == 0 {
				t.Fatalf("%s accepted alias %s", name, alias)
			}
		}
		for _, other := range []string{"os/exec", "unsafe", "golang.org/x/sys/unix"} {
			if len(releaseSourceViolations(map[string]string{name: "package release\nimport \"syscall\"\nimport _ \"" + other + "\"\nvar _ *syscall.Stat_t\n"})) == 0 {
				t.Fatalf("%s accepted import %s", name, other)
			}
		}
	}
	for _, wrong := range []string{"build_checkout_view.go", "build_checkout_view_other.go", "build_checkout_view_linux_test_support.go", "sub/build_checkout_view_linux.go", "check.go"} {
		if len(releaseSourceViolations(map[string]string{wrong: source})) == 0 {
			t.Fatalf("accepted the observer import in %s", wrong)
		}
	}
}
