// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/release"
)

type releaseArtifactMismatch struct{}

func (releaseArtifactMismatch) Error() string { return "release build artifact-mismatch" }

// releaseBuildCommand is registered only after the other release commands hand
// off their shared registration file. The release group remains hidden until WI9.
func releaseBuildCommand(stwVersion ...string) *cobra.Command {
	var o release.BuildOptions
	if len(stwVersion) > 0 {
		o.STWVersion = stwVersion[0]
	}
	var jsonOutput bool
	cmd := &cobra.Command{Use: "build", Short: "Execute declared release commands in CI or an explicit local verification", Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch o.Mode {
			case "ci":
				if !release.IsFullCommitID(o.Commit) || o.Version == "" || o.Out == "" {
					return errors.New("ci requires --commit C --version V --out DIR")
				}
				if o.Tag != "" || o.Record != "" {
					return errors.New("ci does not accept --tag or --record")
				}
				if o.Smoke != "" && o.FilesOnly {
					return errors.New("--smoke and --files-only cannot be combined")
				}
			case "promote":
				if o.Tag == "" || o.Record == "" {
					return errors.New("promote requires --tag T --record FILE")
				}
				if o.Commit != "" || o.Version != "" || o.Out != "" || o.Smoke != "" || o.FilesOnly {
					return errors.New("promote accepts only --tag, --product and --record")
				}
			case "local-verify":
				if !release.IsFullCommitID(o.Commit) || o.Version == "" || o.Record == "" {
					return errors.New("local-verify requires --commit C --version V --record FILE")
				}
				if o.Out != "" || o.Tag != "" || o.Smoke != "" || o.FilesOnly {
					return errors.New("local-verify uses a temporary output directory")
				}
			default:
				return errors.New("--mode must be ci, promote or local-verify")
			}
			for _, value := range []string{o.Version, o.Product, o.Tag, o.Record, o.Out, o.Smoke} {
				if strings.IndexByte(value, 0) >= 0 {
					return errors.New("NUL is not allowed in build inputs")
				}
			}
			r := release.BuildRelease(cmd.Context(), o)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(r)
			} else {
				_, err = io.WriteString(cmd.OutOrStdout(), r.Text())
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if r.Status == "mismatch" {
				return releaseArtifactMismatch{}
			}
			if r.Error != nil {
				return runtimeFailure{fmt.Errorf("release build %s: %s", r.Error.Code, r.Error.Message)}
			}
			return nil
		}}
	f := cmd.Flags()
	f.StringVar(&o.Mode, "mode", "", "ci, promote or local-verify (explicit selection required)")
	f.StringVar(&o.Commit, "commit", "", "Full built commit id")
	f.StringVar(&o.Version, "version", "", "Declared release version")
	f.StringVar(&o.Product, "product", "", "Selected product")
	f.StringVar(&o.Out, "out", "", "New output directory; existing for smoke")
	f.StringVar(&o.Smoke, "smoke", "", "Run only this file target's smoke command")
	f.StringVar(&o.Tag, "tag", "", "Verified release tag for promotion")
	f.StringVar(&o.Record, "record", "", "Canonical record or manifest for comparison")
	f.BoolVar(&o.FilesOnly, "files-only", false, "Skip image targets and their exclusive builds")
	f.BoolVar(&jsonOutput, "json", false, "Output one versioned build result")
	return cmd
}
