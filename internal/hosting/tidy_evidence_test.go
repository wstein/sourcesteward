// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func tidyRepositoryJSON() string {
	return `{"id":41,"node_id":"R_repo","full_name":"owner/repo","default_branch":"main"}`
}
func tidyPRJSON(number int, head, base string) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"PR_%d","number":%d,"state":"closed","merged":true,"merged_at":"2026-10-18T00:00:00Z","draft":false,"head":{"sha":%q,"ref":"topic-%d","repo":{"id":41,"node_id":"R_repo","full_name":"owner/repo"}},"base":{"sha":%q,"ref":%q,"repo":{"id":41,"node_id":"R_repo","full_name":"owner/repo"}},"merge_commit_sha":%q}`, 100+number, number, number, head, number, strings.Repeat("b", 40), base, strings.Repeat("c", 40))
}
func tidyFixtureSession(t *testing.T, handle func(*http.Request) (*http.Response, error)) *TidySession {
	t.Helper()
	return tidyFixtureSessionOver(t, handle)
}

// tidyFixtureSessionOver is a session whose requests go through handle, which may be shared.
func tidyFixtureSessionOver(t *testing.T, handle func(*http.Request) (*http.Response, error)) *TidySession {
	t.Helper()
	ledger := &tidyHTTPLedger{}
	client, err := newSDKClient("github.com", "fixture-secret", t.TempDir(), tidyBudgetTransport{next: roundTripFunc(handle), ledger: ledger})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &TidySession{ctx: ctx, cancel: cancel, cleanup: func() {}, api: &ghAPI{host: "github.com", client: client}, selected: Repository{Host: "github.com", Owner: "owner", Name: "repo"}, repository: TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: TidyRepositoryID{ID: 41, NodeID: "R_repo", FullName: "owner/repo"}, DefaultBranch: "main"}, ledger: ledger}
	t.Cleanup(s.Close)
	return s
}
func tidyBranchJSON(name, oid string) string {
	return fmt.Sprintf(`{"name":%q,"commit":{"sha":%q},"protected":true}`, name, oid)
}

// tidyAliasedMember changes only the JSON spelling of one admitted member.
// The alias-only form tests missing exact members; the second form tests mixed
// exact/alias duplicates without relying on object member order.
func tidyAliasedMember(t *testing.T, raw string, path []string, aliasOnly bool) string {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	object := root
	for _, key := range path[:len(path)-1] {
		next, ok := object[key].(map[string]any)
		if !ok {
			t.Fatal(path)
		}
		object = next
	}
	key := path[len(path)-1]
	object[strings.ToUpper(key)] = object[key]
	if aliasOnly {
		delete(object, key)
	}
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
