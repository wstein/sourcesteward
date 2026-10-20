// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"strings"
	"testing"
)

// Spec 031 mutants M1..M7: each removes one protection from the planning policy
// and the named matrix row must then fail. A surviving mutant means the matrix
// proves nothing about that protection.
func withPolicyMutation(t *testing.T, mutate func(*planningPolicy)) {
	t.Helper()
	original := planningPolicyFactory
	planningPolicyFactory = func() planningPolicy {
		p := original()
		p.arguments = append([]string{}, p.arguments...)
		mutate(&p)
		return p
	}
	t.Cleanup(func() { planningPolicyFactory = original })
}

func dropArgument(p *planningPolicy, item string) {
	out := []string{}
	for i := 0; i < len(p.arguments); i++ {
		if p.arguments[i] == "-c" && p.arguments[i+1] == item {
			i++
			continue
		}
		out = append(out, p.arguments[i])
	}
	p.arguments = out
}

func requireMutantKilled(t *testing.T, row func(*testing.T) error) {
	t.Helper()
	if err := row(t); err == nil {
		t.Fatal("mutant survived: the matrix row still passes")
	} else {
		t.Logf("killed: %v", err)
	}
}

func TestConfigurationPolicyMutants(t *testing.T) {
	inheritCaller := func(bin, _, _ string) []string { return sandboxEnvironment(bin) }
	t.Run("M1 drop core.hooksPath", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { dropArgument(p, "core.hooksPath="+os.DevNull) })
		requireMutantKilled(t, rowC1)
	})
	t.Run("M2 inherit global config", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { p.environment = inheritCaller })
		requireMutantKilled(t, rowC2)
	})
	t.Run("M3 inherit GIT_CONFIG_PARAMETERS", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) {
			p.environment = func(bin, home, xdg string) []string {
				return append(planningEnvironment(bin, home, xdg), "GIT_CONFIG_PARAMETERS='core.hooksPath'='/nonexistent-hooks'")
			}
		})
		requireMutantKilled(t, rowC1)
	})
	t.Run("M3 inherit GIT_CONFIG_COUNT", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) {
			p.environment = func(bin, home, xdg string) []string {
				return append(planningEnvironment(bin, home, xdg), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0=/nonexistent-hooks")
			}
		})
		requireMutantKilled(t, rowC1)
	})
	t.Run("M4 drop merge.renormalize", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { dropArgument(p, "merge.renormalize=false") })
		requireMutantKilled(t, rowRenormalize)
	})
	t.Run("M4 drop core.autocrlf", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { dropArgument(p, "core.autocrlf=false") })
		requireMutantKilled(t, rowConstants)
	})
	t.Run("M5 omit --attr-source", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { p.attrSource = func(string) []string { return nil } })
		requireMutantKilled(t, rowC30)
	})
	t.Run("M6 inherit HOME and XDG_CONFIG_HOME", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) {
			p.environment = func(bin, _, _ string) []string {
				return append(sandboxBaseEnvironment(bin), "HOME="+os.Getenv("HOME"), "XDG_CONFIG_HOME="+os.Getenv("XDG_CONFIG_HOME"), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1")
			}
		})
		requireMutantKilled(t, rowC32)
	})
	t.Run("M6 drop GIT_ATTR_NOSYSTEM", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) {
			p.environment = func(bin, home, xdg string) []string {
				out := []string{}
				for _, item := range planningEnvironment(bin, home, xdg) {
					if !strings.HasPrefix(item, "GIT_ATTR_NOSYSTEM=") {
						out = append(out, item)
					}
				}
				return out
			}
		})
		requireMutantKilled(t, rowC34)
	})
	t.Run("M7 ignore info/attributes", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { p.scanInfoAttributes = false })
		requireMutantKilled(t, rowC31)
	})
	t.Run("drop log.showSignature", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { dropArgument(p, "log.showSignature=false") })
		requireMutantKilled(t, rowSignature)
	})
	for _, c := range contractPlanningConstants {
		t.Run("drop "+c.key, func(t *testing.T) {
			withPolicyMutation(t, func(p *planningPolicy) { dropArgument(p, c.key+"="+c.value) })
			requireMutantKilled(t, rowConstants)
		})
	}
	t.Run("M5 filter evidence reads worktree attributes", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { p.attrSource = func(string) []string { return nil } })
		requireMutantKilled(t, rowFilterAttributeSource)
	})
	t.Run("drop an item of the constant list", func(t *testing.T) {
		withPolicyMutation(t, func(p *planningPolicy) { dropArgument(p, "gc.auto=0") })
		requireMutantKilled(t, rowC27)
	})
}
