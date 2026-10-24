package listing

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixtureHTML(t *testing.T) string {
	t.Helper()
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	p := filepath.Join(filepath.Dir(src), "..", "..", "..",
		"tests", "fixtures", "price-monitoring-listing.html")
	data, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFindLatestPicksOctober232026(t *testing.T) {
	found := FindLatestFromHTML(fixtureHTML(t), ListingURL)
	if found == nil {
		t.Fatal("no link found")
	}
	base := found.URL
	name := base[strings.LastIndex(base, "/")+1:]
	if name != "Price-Monitoring-October-23-2026.pdf" {
		t.Fatalf("url = %q, want Price-Monitoring-October-23-2026.pdf", found.URL)
	}
	if found.Date.ISODate() != "2026-10-23" {
		t.Fatalf("date = %q, want 2026-10-23", found.Date.ISODate())
	}
}

func TestExcludesCigarette(t *testing.T) {
	found := FindLatestFromHTML(fixtureHTML(t), ListingURL)
	if found == nil {
		t.Fatal("no link found")
	}
	if strings.Contains(strings.ToLower(found.URL), "cigarette") {
		t.Fatalf("cigarette link selected: %q", found.URL)
	}
}

func TestExcludesDailyIndexAndWeeklyAverage(t *testing.T) {
	urls := []string{
		"https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-October-23-2026.pdf",
		"https://www.da.gov.ph/wp-content/uploads/2026/10/Daily-Price-Index-October-23-2026.pdf",
		"https://www.da.gov.ph/wp-content/uploads/2026/10/Weekly-Average-Prices-October-12-18-2026.pdf",
		"https://www.da.gov.ph/wp-content/uploads/2026/10/Daily-Cigarette-Price-Monitoring-October-23-2026.pdf",
	}
	found := FindLatestFromURLs(urls)
	if found == nil {
		t.Fatal("no link found")
	}
	if !strings.HasSuffix(found.URL, "Price-Monitoring-October-23-2026.pdf") {
		t.Fatalf("url = %q", found.URL)
	}
}

func TestRevisedBeatsNonRevised(t *testing.T) {
	base := "https://www.da.gov.ph/wp-content/uploads/2026/10/"
	urls := []string{
		base + "Price-Monitoring-October-13-2026.pdf",
		base + "Revised-Price-Monitoring-October-13-2026.pdf",
	}
	found := FindLatestFromURLs(urls)
	if found == nil || !strings.HasSuffix(found.URL, "Revised-Price-Monitoring-October-13-2026.pdf") {
		t.Fatalf("got %+v", found)
	}
	rev := []string{urls[1], urls[0]}
	found = FindLatestFromURLs(rev)
	if found == nil || !strings.HasSuffix(found.URL, "Revised-Price-Monitoring-October-13-2026.pdf") {
		t.Fatalf("order-dependent: %+v", found)
	}
}

func TestSuffixOrdering(t *testing.T) {
	base := "https://www.da.gov.ph/wp-content/uploads/2026/10/"
	urls := []string{
		base + "Price-Monitoring-October-23-2026.pdf",
		base + "Price-Monitoring-October-23-2026-1.pdf",
		base + "Price-Monitoring-October-23-2026-2.pdf",
	}
	found := FindLatestFromURLs(urls)
	if found == nil || !strings.HasSuffix(found.URL, "Price-Monitoring-October-23-2026-2.pdf") {
		t.Fatalf("got %+v", found)
	}
	d := ParseFilenameDate("Price-Monitoring-October-23-2026-2.pdf")
	if d == nil || d.ISODate() != "2026-10-23" {
		t.Fatalf("date = %+v", d)
	}
}

func TestParseFilenameDateVariants(t *testing.T) {
	cases := map[string]string{
		"Price-Monitoring-October-23-2026.pdf":         "2026-10-23",
		"Revised-Price-Monitoring-October-13-2026.pdf": "2026-10-13",
		"Price-Monitoring-December-6-2023-Revised.pdf": "2023-12-06",
		"Updated-Price-Monitoring-May-22-2023.pdf":     "2023-05-22",
		"Price-Monitoring-October-23-2026-1.pdf":       "2026-10-23",
		"Price-Monitoring-October-23-2026.pdf?x=1":     "2026-10-23",
	}
	for in, want := range cases {
		d := ParseFilenameDate(in)
		if d == nil || d.ISODate() != want {
			t.Errorf("ParseFilenameDate(%q) = %+v, want %q", in, d, want)
		}
	}
	for _, in := range []string{
		"Daily-Cigarette-Price-Monitoring-October-23-2026.pdf",
		"Daily-Price-Index-October-23-2026.pdf",
		"Weekly-Average-Prices-October-12-18-2026.pdf",
		"Price-Watch-October-23-2026.pdf",
		"RETAIL-Price-Monitoring-Report_04OCT19.pdf",
		"not-a-report.pdf",
	} {
		if d := ParseFilenameDate(in); d != nil {
			t.Errorf("ParseFilenameDate(%q) = %v, want nil", in, d.ISODate())
		}
		if IsCandidateFilename(in) {
			t.Errorf("IsCandidateFilename(%q) = true, want false", in)
		}
	}
}

func TestFindAllGroupsByDateNewestFirst(t *testing.T) {
	urls := []string{
		"https://www.da.gov.ph/x/Price-Monitoring-October-23-2026.pdf",
		"https://www.da.gov.ph/x/Price-Monitoring-October-23-2026-1.pdf",
		"https://www.da.gov.ph/x/Revised-Price-Monitoring-October-13-2026.pdf",
		"https://www.da.gov.ph/x/Price-Monitoring-October-13-2026.pdf",
		"https://www.da.gov.ph/x/Daily-Cigarette-Price-Monitoring-October-23-2026.pdf",
	}
	all := FindAllFromURLs(urls)
	if len(all) != 2 {
		t.Fatalf("got %d entries: %+v", len(all), all)
	}
	if all[0].Date.ISODate() != "2026-10-23" || all[1].Date.ISODate() != "2026-10-13" {
		t.Fatalf("order wrong: %+v", all)
	}
	if !strings.HasSuffix(all[0].URL, "Price-Monitoring-October-23-2026-1.pdf") {
		t.Fatalf("suffix winner wrong: %q", all[0].URL)
	}
	if !strings.HasSuffix(all[1].URL, "Revised-Price-Monitoring-October-13-2026.pdf") {
		t.Fatalf("revised winner wrong: %q", all[1].URL)
	}
}

func TestFindAllFromHTMLRelativeURLs(t *testing.T) {
	htmlText := `<html><body>
<a href="/wp-content/uploads/2026/10/Price-Monitoring-October-23-2026.pdf">a</a>
<A HREF='https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-October-22-2026.pdf'>b</A>
<a href="https://www.da.gov.ph/x/Daily-Cigarette-Price-Monitoring-October-23-2026.pdf">c</a>
<a href="https://www.da.gov.ph/x/Price-Watch-October-23-2026.pdf">d</a>
<a href="https://www.da.gov.ph/x/RETAIL-Price-Monitoring-Report_04OCT19.pdf">e</a>
<a href="https://www.da.gov.ph/x/Daily-Price-Index-October-23-2026.pdf">f</a>
<a href="https://www.da.gov.ph/x/Weekly-Average-Prices-October-12-18-2026.pdf">g</a>
</body></html>`
	all := FindAllFromHTML(htmlText, ListingURL)
	if len(all) != 2 {
		t.Fatalf("got %+v", all)
	}
	if all[0].Date.ISODate() != "2026-10-23" || all[1].Date.ISODate() != "2026-10-22" {
		t.Fatalf("wrong: %+v", all)
	}
	if all[0].URL != "https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-October-23-2026.pdf" {
		t.Fatalf("urljoin wrong: %q", all[0].URL)
	}
}
