// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"strings"
)

// Placeholders are a closed set, with one allowed subset per field (spec placeholder table).
func nameSet(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

var (
	placeholdersBuild          = nameSet("version", "version_number", "commit", "commit12", "source_date_epoch", "out_dir")
	placeholdersBuildPerTarget = nameSet("version", "version_number", "commit", "commit12", "source_date_epoch", "out_dir", "os", "arch", "target", "output")
	placeholdersOutput         = nameSet("version", "version_number", "commit12", "os", "arch", "target")
	placeholdersStagingTag     = nameSet("commit", "commit12")
	placeholdersSmoke          = nameSet("version", "version_number", "commit", "os", "arch", "target", "artifact", "out_dir")
	placeholdersPromote        = nameSet("repository", "digest", "version", "version_number")
	placeholdersURL            = nameSet("repository", "tag", "name")
)

// placeholderProblem returns why a string uses a brace expression that is not an allowed
// placeholder, or "". Only the first problem is reported.
func placeholderProblem(s string, allowed map[string]bool) string {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '}':
			return "a closing brace without an opening brace"
		case '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return "an opening brace without a closing brace"
			}
			name := s[i+1 : i+end]
			if !allowed[name] {
				if strings.ContainsAny(name, "{ ") || name == "" {
					return fmt.Sprintf("the brace expression {%s} is not a placeholder", clip(name))
				}
				return fmt.Sprintf("the placeholder {%s} is unknown or not allowed in this field", clip(name))
			}
			i += end
		}
	}
	return ""
}

// clip shortens repository text echoed in a message.
func clip(s string) string {
	runes := []rune(s)
	if len(runes) > 48 {
		return string(runes[:48]) + "..."
	}
	return s
}
