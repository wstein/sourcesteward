package release

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain refuses to run when the process inherits a GIT_DIR that is not a
// temporary repository and removes every other repository-selecting variable, so
// that a fixture can never write into a real repository.
func TestMain(m *testing.M) {
	if err := isolateGitEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "release tests refuse to run:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

// inheritedGitVariables select a repository, an index, objects or
// configuration for every git command, so fixtures must never inherit them.
var inheritedGitVariables = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_REFLOG_ACTION"}

// withoutInheritedGit returns env without repository-selecting variables
// and without any GIT_CONFIG* variable.
func withoutInheritedGit(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := strings.HasPrefix(name, "GIT_CONFIG")
		for _, bad := range inheritedGitVariables {
			drop = drop || name == bad
		}
		if !drop {
			kept = append(kept, kv)
		}
	}
	return kept
}
