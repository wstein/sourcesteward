// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The architecture rules of internal/release, enforced on the syntax tree (not on text), so
// spacing, line breaks, aliases and unusual formatting cannot hide a use:
//   - processes start only in vcs_*.go, attest.go and build.go (os/exec, os.StartProcess);
//   - syscall is imported only by openfile_*.go (and build.go for process groups), without an
//     alias, and in openfile_*.go its only uses are the selector expressions syscall.O_*;
//   - no cgo (import "C", cgo directives), no assembly or other foreign sources, no unsafe, no
//     go:linkname, no plugin or x/sys, no dot or blank import of os, syscall, os/exec or unsafe.
var (
	processFile  = regexp.MustCompile(`^(vcs_[a-z0-9_]+|attest|build)\.go$`)
	openFile     = regexp.MustCompile(`^openfile_[a-z0-9_]+\.go$`)
	foreignFiles = regexp.MustCompile(`(?i)\.(s|c|cc|cpp|cxx|h|hh|hpp|m|f|f90|syso|swig|swigcxx)$`)
)

// releaseSourceViolations checks the files of internal/release (name -> content).
func releaseSourceViolations(files map[string]string) []string {
	var out []string
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
			switch {
			case path == "C":
				add(name, `import "C" (cgo) is not allowed`)
			case path == "unsafe", path == "plugin", strings.HasPrefix(path, "golang.org/x/sys"):
				add(name, "import of "+path+" is not allowed")
			case path == "syscall":
				if !openFile.MatchString(name) && name != "build.go" {
					add(name, "syscall may be imported only by openfile_*.go and build.go")
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
		allowedSyscall := map[*ast.Ident]bool{}
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
			if x.Name == osName && sel.Sel.Name == "StartProcess" && !processFile.MatchString(name) {
				add(name, "os.StartProcess is not allowed outside vcs_*.go, attest.go and build.go")
			}
			return true
		})
		if openFile.MatchString(name) {
			imports := map[*ast.Ident]bool{}
			for _, spec := range file.Imports {
				if spec.Name != nil {
					imports[spec.Name] = true
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "syscall" && !allowedSyscall[id] && !imports[id] {
					add(name, "the only use of syscall allowed here is a syscall.O_ constant, found "+fset.Position(id.Pos()).String())
				}
				return true
			})
		}
	}
	sort.Strings(out)
	return out
}

func TestReleaseSourceArchitecture(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(raw)
	}
	if v := releaseSourceViolations(files); len(v) != 0 {
		t.Fatalf("architecture violations:\n%s", strings.Join(v, "\n"))
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
