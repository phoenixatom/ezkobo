package book

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	_ "golang.org/x/crypto/x509roots/fallback" // CA roots if the Kobo has none
)

// fixMetadata returns improved metadata, or nil if nothing changed. With
// online set, it looks the book up (Google Books, then Open Library) when
// the title or author is missing or messy, or the book has no cover.
func fixMetadata(ctx context.Context, m *epubMeta, fileName string, st Options) (*epubMeta, string) {
	online := st.Metadata
	fixed := *m
	source := ""

	// Without metadata, fall back to the file name, which is often
	// "Title (Author) (site.tld).epub".
	if isMessy(fixed.Title) {
		src := cleanText(m.Title)
		if src == "" {
			src = cleanText(strings.TrimSuffix(strings.TrimSuffix(fileName, filepath.Ext(fileName)), ".kepub"))
		}
		fixed.Title = src
		// "Title (Author)": only split when there's no real author, so titles
		// like "The Hobbit (Illustrated Edition)" stay intact.
		if isMessy(fixed.Author) {
			if title, author := splitTitleAuthor(src); author != "" {
				fixed.Title, fixed.Author = title, author
			}
		}
	}
	fixed.Title = cleanText(fixed.Title)
	fixed.Author = cleanText(fixed.Author)
	if fixed.Title != m.Title || fixed.Author != m.Author {
		source = "cleaned"
	}

	needsLookup := isMessy(m.Title) || isMessy(m.Author) || !m.HasCover
	if online && needsLookup && fixed.Title != "" {
		lctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if r, src := lookupBook(lctx, fixed.Title, fixed.Author, st); r != nil {
			// Covers redirect a few times and can take several seconds.
			cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			if isMessy(m.Title) && r.Title != "" {
				fixed.Title = r.Title
			}
			if isMessy(m.Author) && r.Author != "" {
				fixed.Author = r.Author
			}
			if !m.HasCover && r.CoverURL != "" {
				fixed.Cover = fetchCover(cctx, r.CoverURL)
			}
			source = src
		}
	}
	if source == "" && fixed.Cover == nil {
		return nil, ""
	}
	return &fixed, source
}

type bookInfo struct {
	Title, Author, CoverURL string
}

var httpClient = &http.Client{Timeout: 12 * time.Second}

const userAgent = "EzKobo (+https://github.com/phoenixatom/ezkobo)"

// lookupBook asks each enabled provider in the user's order and returns the
// first good match.
func lookupBook(ctx context.Context, title, author string, st Options) (*bookInfo, string) {
	for _, p := range st.Providers {
		if !p.Enabled || ctx.Err() != nil {
			continue
		}
		var b *bookInfo
		switch p.ID {
		case "apple":
			b = lookupApple(ctx, title, author)
		case "openlibrary":
			b = lookupOpenLibrary(ctx, title, author)
		case "google":
			b = lookupGoogle(ctx, title, author, st.GoogleAPIKey)
		case "hardcover":
			if st.HardcoverToken != "" {
				b = lookupHardcover(ctx, title, author, st.HardcoverToken)
			}
		}
		if b != nil {
			return b, p.ID
		}
	}
	return nil, ""
}

// firstAuthor keeps the main author from lists like "Asako Yuzuki & Polly
// Barton" (translators are often listed too).
func firstAuthor(s string) string {
	s, _, _ = strings.Cut(s, " & ")
	s, _, _ = strings.Cut(s, ",")
	return strings.TrimSpace(s)
}

// lookupApple uses the iTunes Search API's ebook catalogue (Apple Books).
func lookupApple(ctx context.Context, title, author string) *bookInfo {
	v := url.Values{"media": {"ebook"}, "entity": {"ebook"}, "limit": {"5"}, "term": {strings.TrimSpace(title + " " + author)}}
	var res struct {
		Results []struct {
			Title   string `json:"trackName"`
			Artist  string `json:"artistName"`
			Artwork string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if getJSON(ctx, "https://itunes.apple.com/search?"+v.Encode(), &res) != nil {
		return nil
	}
	for _, r := range res.Results {
		if !sameTitle(title, r.Title) {
			continue
		}
		b := &bookInfo{Title: r.Title, Author: firstAuthor(r.Artist)}
		if r.Artwork != "" {
			b.CoverURL = strings.Replace(r.Artwork, "100x100bb", "1000x1000bb", 1)
		}
		return b
	}
	return nil
}

// lookupHardcover uses Hardcover's GraphQL search (needs an API token).
func lookupHardcover(ctx context.Context, title, author, token string) *bookInfo {
	body, _ := json.Marshal(map[string]any{
		"query":     `query ($q: String!) { search(query: $q, query_type: "Book", per_page: 5, page: 1) { results } }`,
		"variables": map[string]string{"q": strings.TrimSpace(title + " " + author)},
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.hardcover.app/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var res struct {
		Data struct {
			Search struct {
				Results struct {
					Hits []struct {
						Document struct {
							Title       string   `json:"title"`
							AuthorNames []string `json:"author_names"`
							Image       struct {
								URL string `json:"url"`
							} `json:"image"`
						} `json:"document"`
					} `json:"hits"`
				} `json:"results"`
			} `json:"search"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&res) != nil {
		return nil
	}
	for _, h := range res.Data.Search.Results.Hits {
		d := h.Document
		if !sameTitle(title, d.Title) {
			continue
		}
		b := &bookInfo{Title: d.Title, CoverURL: d.Image.URL}
		if len(d.AuthorNames) > 0 {
			b.Author = d.AuthorNames[0]
		}
		return b
	}
	return nil
}

func lookupGoogle(ctx context.Context, title, author, key string) *bookInfo {
	q := "intitle:" + title
	if author != "" {
		q += " inauthor:" + author
	}
	var res struct {
		Items []struct {
			VolumeInfo struct {
				Title      string   `json:"title"`
				Subtitle   string   `json:"subtitle"`
				Authors    []string `json:"authors"`
				ImageLinks struct {
					Thumbnail string `json:"thumbnail"`
				} `json:"imageLinks"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	u := "https://www.googleapis.com/books/v1/volumes?maxResults=5&q=" + url.QueryEscape(q)
	if key != "" {
		u += "&key=" + url.QueryEscape(key)
	}
	if getJSON(ctx, u, &res) != nil {
		return nil
	}
	for _, it := range res.Items {
		v := it.VolumeInfo
		if !sameTitle(title, v.Title) {
			continue
		}
		b := &bookInfo{Title: v.Title}
		if len(v.Authors) > 0 {
			b.Author = v.Authors[0]
		}
		if t := v.ImageLinks.Thumbnail; t != "" {
			// Ask for a larger image than the default thumbnail.
			t = strings.Replace(strings.Replace(t, "http://", "https://", 1), "&edge=curl", "", 1)
			b.CoverURL = strings.Replace(t, "zoom=1", "zoom=3", 1)
		}
		return b
	}
	return nil
}

func lookupOpenLibrary(ctx context.Context, title, author string) *bookInfo {
	v := url.Values{"title": {title}, "limit": {"5"}, "fields": {"title,author_name,cover_i"}}
	if author != "" {
		v.Set("author", author)
	}
	var res struct {
		Docs []struct {
			Title   string   `json:"title"`
			Authors []string `json:"author_name"`
			Cover   int      `json:"cover_i"`
		} `json:"docs"`
	}
	if getJSON(ctx, "https://openlibrary.org/search.json?"+v.Encode(), &res) != nil {
		return nil
	}
	for _, d := range res.Docs {
		if !sameTitle(title, d.Title) {
			continue
		}
		b := &bookInfo{Title: d.Title}
		if len(d.Authors) > 0 {
			b.Author = d.Authors[0]
		}
		if d.Cover > 0 {
			b.CoverURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", d.Cover)
		}
		return b
	}
	return nil
}

// sameTitle guards against applying a different book's metadata: the
// result's title must contain the searched title's words (or vice versa).
func sameTitle(a, b string) bool {
	na, nb := normalize(a), normalize(b)
	return na != "" && nb != "" && (strings.Contains(nb, na) || strings.Contains(na, nb))
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func getJSON(ctx context.Context, u string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(v)
}

func fetchCover(ctx context.Context, u string) []byte {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	// Must be a real JPEG of reasonable size (placeholders are tiny).
	if err != nil || resp.StatusCode != http.StatusOK || len(b) < 2048 || !bytes.HasPrefix(b, []byte{0xFF, 0xD8}) {
		return nil
	}
	return b
}
