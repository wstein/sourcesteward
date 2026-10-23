// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// statusSchema compiles schemas/v2/status.schema.json without any network or external reference.
func statusSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v2", "status.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	const id = "https://wstein.github.io/sourcesteward/schemas/v2/status.schema.json"
	c := jsonschema.NewCompiler()
	c.UseLoader(noLoader{})
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type noLoader struct{}

func (noLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema loading forbidden: %s", url)
}

// requireValid marshals the document the way the command prints it and validates it. The schema forbids
// additional properties, so a key of the document that the schema does not know fails here too.
func requireValid(t *testing.T, name string, d Document) {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(marshal(t, d)))
	if err != nil {
		t.Fatal(err)
	}
	if err := statusSchema(t).Validate(v); err != nil {
		t.Errorf("%s: the document does not validate: %v", name, err)
	}
}

// Every shape the command can print validates: all sources read, no token, a failing forge, a rate
// limit, a deadline skip with failures elsewhere, a repository that is not on GitHub and a failed run.
func TestDocumentsOfEveryOutcomeValidateAgainstTheSchema(t *testing.T) {
	requireValid(t, "fixture workspace", fixtureReport().Document())

	rep, _ := runWith(t, &forgeFake{draft: true}, nil, nil)
	requireValid(t, "everything read, a draft", rep.Document())

	rep, _ = runWith(t, &forgeFake{}, fmt.Errorf("gh: %w", hosting.ErrAuthentication), nil)
	requireValid(t, "no usable token", rep.Document())

	rep, _ = runWith(t, &forgeFake{}, errors.New("no gh"), nil)
	requireValid(t, "a reader that cannot be opened", rep.Document())

	rep, _ = runWith(t, &forgeFake{err: &hosting.WatchError{Code: hosting.CodeNetwork, Message: "boom"}}, nil, nil)
	requireValid(t, "every request fails", rep.Document())

	rep, _ = runWith(t, &forgeFake{err: &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "limit"}}, nil, nil)
	requireValid(t, "the rate limit is reached", rep.Document())

	failedLocal := Report{Time: runTime, Local: stwsync.StatusResult{Repos: []stwsync.RepoStatus{}, Error: &stwsync.Failure{Code: "status-not-a-repository", Message: "no"}}}
	failedLocal.Overview = overview.Build(overview.Input{})
	requireValid(t, "a failed local read", failedLocal.Document())
}

func TestADocumentWithAnErrorSourceOfEveryCodeValidates(t *testing.T) {
	codes := []string{"unauthorized", "forbidden", "rate-limited", "network", "http-5xx", "malformed-response", "page-too-large", "repo-moved",
		"too-many-releases", "too-many-open-prs", statusremote.CodeContractInvalid, statusremote.CodeHostingUnavailable}
	for _, code := range codes {
		rep := fixtureReport()
		for _, src := range overview.StatusSources {
			rep.Remote.Repos[1].Remote.Sources[src] = overview.SourceState{Outcome: overview.OutcomeError, Code: code}
		}
		rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
		requireValid(t, "error code "+code, rep.Document())
	}
	for _, reason := range []string{overview.ReasonNoToken, overview.ReasonDeadline, overview.ReasonNotFound, overview.ReasonOffline, overview.ReasonNotAttempted,
		overview.ReasonNotGitHub, overview.ReasonOtherHost, overview.ReasonNotReadLocally, overview.ReasonInvalidRepo} {
		rep := fixtureReport()
		for _, src := range overview.StatusSources {
			rep.Remote.Repos[1].Remote.Sources[src] = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: reason}
		}
		rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
		requireValid(t, "skip reason "+reason, rep.Document())
	}
}
