// Package i18nguard holds a single regression guard: the whole repository (code, docs, UI) must be
// English. The project did an i18n-to-English pass; this test fails the build if French creeps back
// in. This file is the one place that necessarily names French letters/words, so the walk skips any
// path containing "i18nguard". Data files (.rules, testdata) and the vendored fork (third_party) are
// out of scope.
package i18nguard

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// frenchAccents are Latin letters with diacritics common in French and essentially absent from
// English technical prose.
const frenchAccents = "àâäçéèêëîïôöùûüœæ" +
	"ÀÂÄÇÉÈÊËÎÏÔÖÙÛÜŒÆ"

// frenchWords are unambiguous French function-words that are NOT English homographs, matched
// whole-word (lowercased). Content words (règle, modèle, décision, vérifié, défaut, chaîne…) carry
// accents in French and are caught by the accent check, so they are deliberately omitted here to
// avoid colliding with their English spellings ("decision", "value", …). Kept tight on purpose.
var frenchWords = map[string]bool{
	"les": true, "une": true, "des": true, "sont": true, "avec": true, "dans": true,
	"mais": true, "donc": true, "nous": true, "vous": true, "leur": true, "leurs": true,
	"cette": true, "ces": true, "cela": true, "ceci": true, "sinon": true, "alors": true,
	"lorsque": true, "afin": true, "puisque": true, "ainsi": true, "voici": true,
}

// scanExt are the file extensions whose text must be English.
var scanExt = map[string]bool{".go": true, ".md": true, ".html": true, ".js": true, ".css": true}

func TestRepositoryIsEnglish(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	accents := map[rune]bool{}
	for _, r := range frenchAccents {
		accents[r] = true
	}

	var violations []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "third_party", "dist", "node_modules", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !scanExt[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if strings.Contains(path, "i18nguard") { // this guard names French words on purpose
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		rel, _ := filepath.Rel(root, path)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
		for ln := 1; sc.Scan(); ln++ {
			line := sc.Text()
			for _, r := range line {
				if accents[r] {
					violations = append(violations, rel+":"+itoa(ln)+" — French accented letter "+string(r)+" in: "+trim(line))
					break
				}
			}
			lower := strings.ToLower(line)
			for _, w := range splitWords(lower) {
				if frenchWords[w] {
					violations = append(violations, rel+":"+itoa(ln)+" — French word \""+w+"\" in: "+trim(line))
					break
				}
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("non-English text found (the repo must be English-only):\n  %s", strings.Join(violations, "\n  "))
	}
}

// splitWords yields lowercase alphabetic tokens (letters only) from a line.
func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
