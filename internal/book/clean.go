package book

import (
	"regexp"
	"strings"
)

// junk matches a bracketed website tag that download sites append to titles
// and file names, e.g. "(example.org, mirror.net)" or "[site.com]".
var junk = regexp.MustCompile(`(?i)\s*[(\[][^()\[\]]*\b[a-z0-9-]+\.[a-z]{2,6}\b[^()\[\]]*[)\]]`)

func isMessy(s string) bool {
	return s == "" || strings.EqualFold(s, "unknown") || junk.MatchString(s) || strings.Contains(s, "_")
}

func cleanText(s string) string {
	s = junk.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.Join(strings.Fields(s), " ")
}

// splitTitleAuthor splits "Title (Author)" into its parts, handling nested
// brackets like "Butter (Asako Yuzuki, Polly Barton (translator))". Only the
// first author is kept.
func splitTitleAuthor(s string) (title, author string) {
	if !strings.HasSuffix(s, ")") {
		return s, ""
	}
	depth := 0
	for i := len(s) - 1; i > 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			depth--
		}
		if depth == 0 {
			title = strings.TrimSpace(s[:i])
			author = s[i+1 : len(s)-1]
			break
		}
	}
	if title == "" {
		return s, ""
	}
	author, _, _ = strings.Cut(author, ",")
	author, _, _ = strings.Cut(author, " (")
	author = strings.TrimSuffix(strings.TrimSpace(author), " etc.")
	return title, strings.TrimSpace(author)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
