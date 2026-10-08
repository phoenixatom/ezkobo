// Package book prepares a book for the Kobo as it arrives: it cleans up
// messy metadata, looks up missing details online, converts EPUBs to KEPUB
// and picks a tidy file name.
package book

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
)

// Options control what happens to a book when it arrives.
type Options struct {
	// Convert EPUBs to Kobo's KEPUB format on arrival.
	Kepub bool `json:"kepub"`
	// Look up title, author and cover on Open Library when a book's own
	// metadata is missing or messy. Sends the title and author to Open Library.
	Metadata bool `json:"metadata"`
	// Save books as "Author - Title".
	CleanNames bool `json:"cleanNames"`
	// Where to look up details, in order. Each can be switched off.
	Providers []Provider `json:"providers"`
	// Name shown in the app, e.g. "Atom's Libra". Empty means automatic.
	Name string `json:"name,omitempty"`
	// Optional Google Books API key; keyless requests share a public daily
	// quota that is often used up. Never sent back to clients.
	GoogleAPIKey string `json:"googleApiKey,omitempty"`
	// Hardcover API token (required for Hardcover). Never sent back to clients.
	HardcoverToken string `json:"hardcoverToken,omitempty"`
}

// Provider is a source of book details, which can be switched off.
type Provider struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// Default order, best first for recent and translated books. Hardcover needs
// a token, so it's off until one is added.
var AllProviders = []Provider{
	{"apple", true}, {"openlibrary", true}, {"google", true}, {"hardcover", false},
}

// NormalizeProviders keeps known providers in the given order, then appends
// any missing ones with their defaults.
func NormalizeProviders(ps []Provider) []Provider {
	var out []Provider
	seen := map[string]bool{}
	for _, p := range ps {
		for _, known := range AllProviders {
			if p.ID == known.ID && !seen[p.ID] {
				out = append(out, p)
				seen[p.ID] = true
			}
		}
	}
	for _, known := range AllProviders {
		if !seen[known.ID] {
			out = append(out, known)
		}
	}
	return out
}

// Result describes what happened to an uploaded book.
type Result struct {
	Name      string `json:"name"`      // final file name
	Converted bool   `json:"converted"` // EPUB → KEPUB
	Metadata  string `json:"metadata"`  // "", "cleaned", "google", "openlibrary"
}

// Process applies the Kobo's settings to an uploaded file at tmp, which
// was sent as name. It may rewrite tmp in place and returns the final name.
func Process(ctx context.Context, st Options, tmp, name string) Result {
	res := Result{Name: name}
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".epub") {
		return res // only EPUBs carry metadata we can fix or convert
	}
	isKepub := strings.HasSuffix(lower, ".kepub.epub")

	meta, err := readEPUBMeta(tmp)
	if err != nil {
		log.Printf("ezkobo: %s: reading metadata: %v", name, err)
		return res
	}

	if st.Metadata || st.CleanNames {
		if fixed, source := fixMetadata(ctx, meta, name, st); fixed != nil {
			if err := writeEPUBMeta(tmp, fixed); err != nil {
				log.Printf("ezkobo: %s: writing metadata: %v", name, err)
			} else {
				meta, res.Metadata = fixed, source
			}
		}
	}

	if st.Kepub && !isKepub {
		if err := convertKepub(ctx, tmp); err != nil {
			log.Printf("ezkobo: %s: KEPUB conversion failed, keeping EPUB: %v", name, err)
		} else {
			res.Converted, isKepub = true, true
		}
	}

	ext := ".epub"
	if isKepub {
		ext = ".kepub.epub"
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, filepath.Ext(name)), ".kepub")
	if st.CleanNames && meta.Title != "" {
		base = meta.Title
		if meta.Author != "" {
			base = meta.Author + " - " + meta.Title
		}
	}
	if n, err := CleanName(base + ext); err == nil {
		res.Name = n
	}
	return res
}

// Formats Kobo's stock reader can open.
var bookExts = map[string]bool{
	".epub": true, ".pdf": true, ".mobi": true, ".cbz": true, ".cbr": true,
	".txt": true, ".html": true, ".htm": true, ".rtf": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".tif": true, ".tiff": true,
}

// CleanName turns a client-supplied file name into a safe FAT32 file name
// inside the books folder.
func CleanName(n string) (string, error) {
	n = path.Base(strings.ReplaceAll(n, `\`, "/"))
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, n)
	n = strings.TrimSpace(strings.TrimRight(n, ". "))
	if n == "" || strings.HasPrefix(n, ".") {
		return "", errors.New("invalid file name")
	}
	if !IsBook(n) {
		return "", fmt.Errorf("%s: not a format Kobo can open", n)
	}
	if ext := filepath.Ext(n); len(n) > 200 {
		n = strings.ToValidUTF8(n[:200-len(ext)], "") + ext
	}
	return n, nil
}

func IsBook(name string) bool {
	return bookExts[strings.ToLower(filepath.Ext(name))]
}
