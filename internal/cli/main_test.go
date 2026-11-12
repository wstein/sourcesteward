// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"os"
	"testing"

	"github.com/wstein/sourcesteward/internal/gittest"
)

func TestMain(m *testing.M) {
	gittest.LimitBelow(2, 46, "", "^(TestHostingSDKEntryPoints|TestHostingStatusAndLookupEntryPoints|TestPruneV4CLIRefusesUnprovenOrChangedSelection|TestPruneV4CLIStoresOnlyExactOwnedPlan|TestReleaseCheckCommit|TestReleaseCheckEmptyProductIsAbsent|TestReleaseCheckEscapesHostileRepositoryText|TestReleaseCheckFindingsExitThree|TestReleaseCheckJSON|TestReleaseCheckProductAmbiguityExitsThree|TestReleaseCheckRefusalsExitOne|TestReleaseCheckValidContract|TestReleasePlanAllowlistCountsBothFormats|TestReleasePlanGateExitStatuses|TestReleaseRecordAndVerifyCLIEntryPoints|TestReleaseRecordExplicitZeroRunIDIsCompared|TestReleaseRecordExplicitZeroRunIDMayMatchManifest|TestRestackPlanEndToEnd|TestTidyOverviewV4NativeReadOnlyMissingRegistration|TestTidyPruneV4ApplyExplicitDispatch|TestTidyPruneV4RestoreExplicitDispatch|TestTidyPruneV4RestoreHumanDisclosesLimitations)$")
	if err := isolateGitEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "tests refuse to run:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}
