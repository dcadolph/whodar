package index

import (
	"strings"

	"github.com/kljensen/snowball"
)

// stopwords are common words ignored during tokenization. They include the
// filler words found in questions like "who do I talk to about X".
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "who": true, "are": true,
	"with": true, "about": true, "talk": true, "owns": true, "own": true,
	"what": true, "how": true, "our": true, "you": true, "your": true,
	"this": true, "that": true, "from": true, "does": true, "can": true,
	"do": true, "to": true, "of": true, "in": true, "on": true, "or": true,
	"is": true, "it": true, "we": true, "me": true, "my": true, "an": true,
	"at": true, "be": true, "as": true, "by": true, "us": true, "need": true,
	"help": true, "have": true, "has": true, "get": true, "got": true,
}

// tokenize lowercases text and splits it into searchable tokens, dropping
// stopwords and tokens shorter than two characters.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !isAlphaNum(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if len(f) < 2 || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// isAlphaNum reports whether r is an ASCII letter or digit.
func isAlphaNum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// slug normalizes text into a lowercase identifier with single hyphens between
// runs of alphanumerics. Unlike tokenize, it keeps every word.
func slug(s string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if isAlphaNum(r) {
			b.WriteRune(r)
			hyphen = false
			continue
		}
		if !hyphen && b.Len() > 0 {
			b.WriteByte('-')
			hyphen = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// stem reduces a token to its root for matching, so "scans", "scan", and
// "scanning" share a posting. It is applied only to posting keys and query
// terms, never to displayed text, so reasons and names stay readable.
func stem(token string) string {
	s, err := snowball.Stem(token, "english", true)
	if err != nil || s == "" {
		return token
	}
	return s
}

// containsToken reports whether any phrase contains term as a whole token.
func containsToken(phrases []string, term string) bool {
	for _, ph := range phrases {
		for _, tok := range tokenize(ph) {
			if tok == term {
				return true
			}
		}
	}
	return false
}
