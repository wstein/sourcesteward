// SPDX-License-Identifier: EUPL-1.2
package hosting

import "testing"

func TestEncodeBranchSegment(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"main", "main"},
		{"feat/x", "feat%2Fx"},
		{"release/v1.2", "release%2Fv1.2"},
		{"feature/ünï", "feature%2F%C3%BCn%C3%AF"},
		{"日本", "%E6%97%A5%E6%9C%AC"},
	} {
		got, err := EncodeBranchSegment(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%q: %q %v want %q", tc.in, got, err, tc.want)
		}
		if !branchSegmentValid(got) {
			t.Errorf("own output refused: %q", got)
		}
	}
}

func TestEncodeBranchSegmentRefusesInvalidNames(t *testing.T) {
	long := make([]byte, 256)
	for i := range long {
		long[i] = 'a'
	}
	for _, in := range []string{"", "..", "a..b", "-x", "a@{b", "a/", "/a", "a//b", "x.lock", "a/x.lock", "a\x00b", "a\nb", "a\x7fb", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a%2Fb", "%252F", "\xff", ".hidden", "a/.b", "a.", "@", string(long)} {
		if got, err := EncodeBranchSegment(in); err == nil {
			t.Errorf("accepted %q -> %q", in, got)
		}
	}
}

func TestBranchSegmentGuardRefusesAttackStrings(t *testing.T) {
	for _, seg := range []string{"..%2F", "%252F", "%2e%2e", "%2E%2E", "%00", "%5C", "%5c", "feat%2fx", "feat/x", "feat%2Fx%2F", "a%20b", "ü", "", "%", "%zz", "%2e", "a%2Eb%2E", "feat%252Fx", "x%0Ay", "%7F"} {
		if branchSegmentValid(seg) {
			t.Errorf("accepted raw segment %q", seg)
		}
	}
	for _, seg := range []string{"main", "feat%2Fx", "release%2Fv1.2", "%C3%BC", "feature%2F%C3%BCn%C3%AF"} {
		if !branchSegmentValid(seg) {
			t.Errorf("refused %q", seg)
		}
	}
}
