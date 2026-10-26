// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// Spec 044 SC-010: every reason, proof and risk word has a sentence and an action. The sentence is
// human wording and never carries the code it describes.
func TestEveryWordHasASentenceAndAnAction(t *testing.T) {
	type word struct {
		class, code string
		lookup      func(string) (Sentence, bool)
	}
	var words []word
	for _, r := range tidywizard.AllReasons() {
		words = append(words, word{"reason", r, ReasonSentence})
	}
	for _, p := range tidywizard.AllProofs {
		words = append(words, word{"proof", string(p), ProofSentence})
	}
	for _, r := range risk.AllReasons() {
		words = append(words, word{"risk reason", r, RiskReasonSentence})
	}
	for _, l := range []risk.Level{risk.Green, risk.Yellow, risk.Orange, risk.Red, risk.Kept} {
		words = append(words, word{"level", string(l), func(c string) (Sentence, bool) { return LevelSentence(risk.Level(c)) }})
	}
	if len(words) < 48 {
		t.Fatalf("only %d words are checked", len(words))
	}
	for _, w := range words {
		s, ok := w.lookup(w.code)
		if !ok {
			t.Errorf("%s %q has no sentence", w.class, w.code)
			continue
		}
		if strings.TrimSpace(s.Why) == "" || strings.TrimSpace(s.Action) == "" {
			t.Errorf("%s %q: sentence %q or action %q is empty", w.class, w.code, s.Why, s.Action)
		}
		for _, text := range []string{s.Why, s.Action} {
			if strings.Contains(text, w.code) && w.class != "level" {
				t.Errorf("%s %q: the wording repeats the code: %q", w.class, w.code, text)
			}
			if len(text) > 100 || strings.ContainsAny(text, "\n\x1b") || strings.HasSuffix(text, ".") {
				t.Errorf("%s %q: wording %q is too long, multi-line or ends with a period", w.class, w.code, text)
			}
		}
	}
}

func TestUnknownWordsHaveNoSentence(t *testing.T) {
	for _, f := range []func(string) (Sentence, bool){ReasonSentence, ProofSentence, RiskReasonSentence, func(c string) (Sentence, bool) { return LevelSentence(risk.Level(c)) }} {
		if s, ok := f("no-such-code"); ok || s != (Sentence{}) {
			t.Errorf("unknown word got a sentence: %+v", s)
		}
		if _, ok := f(""); ok {
			t.Error("the empty word got a sentence")
		}
	}
}

// A code the table does not know is never dropped and never raw: the fallback names it escaped and
// still points at the places that keep the code.
func TestFallbackSentenceEscapesTheCode(t *testing.T) {
	s := reasonSentenceOrFallback("new-reason\x1b[2J")
	if strings.ContainsRune(s.Why, 0x1b) || !strings.Contains(s.Why, "new-reason") || s.Action == "" {
		t.Fatalf("fallback: %+v", s)
	}
	if got := reasonSentenceOrFallback("unmerged"); got.Why == s.Why {
		t.Fatal("a known reason must not get the fallback")
	}
}
