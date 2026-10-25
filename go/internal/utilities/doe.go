package utilities

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// Attachment is one DOE listing attachment link.
type Attachment struct {
	Title string
	URL   string
}

// anchorRe finds <a ... href="...">...</a> (single/double quoted,
// case-insensitive), mirroring the listing-page regexp approach used
// for the DA listing (no new dependencies).
var anchorRe = regexp.MustCompile(`(?is)<a\b[^>]*?\bhref\s*=\s*('([^']*)'|"([^"]*)"|([^\s>]+))[^>]*>(.*?)</a>`)

// tagRe strips inner tags from anchor text.
var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// wsRe collapses whitespace.
var wsRe = regexp.MustCompile(`\s+`)

// ParseAttachments extracts DOE media PDF links from a listing page in
// document order: HTML-unescaped title, whitespace collapsed, deduped
// by URL path ignoring the query string (first URL seen wins).
// Relative hrefs resolve against base; only http(s) URLs whose path
// ends in .pdf are kept.
func ParseAttachments(page []byte, base string) []Attachment {
	baseURL, _ := url.Parse(base)
	var out []Attachment
	seen := map[string]bool{}
	for _, m := range anchorRe.FindAllSubmatch(page, -1) {
		raw := anchorHref(m)
		if raw == "" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if baseURL != nil && !u.IsAbs() {
			u = baseURL.ResolveReference(u)
		}
		if !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(u.Path), ".pdf") {
			continue
		}
		key := strings.ToLower(u.Scheme + "://" + u.Host + u.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		title := wsRe.ReplaceAllString(strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(string(m[5]), " "))), " ")
		out = append(out, Attachment{Title: title, URL: u.String()})
	}
	if out == nil {
		return []Attachment{}
	}
	return out
}

// anchorHref returns the href from an anchorRe match's quote variants.
func anchorHref(m [][]byte) string {
	if len(m[2]) > 0 {
		return string(m[2])
	}
	if len(m[3]) > 0 {
		return string(m[3])
	}
	return string(m[4])
}
