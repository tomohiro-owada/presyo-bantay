package fx

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

func connectTmp(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func fixtureBytes(t *testing.T, name ...string) []byte {
	t.Helper()
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	parts := append([]string{filepath.Dir(src), "..", "..", "..",
		"tests", "fixtures"}, name...)
	data, err := os.ReadFile(filepath.Clean(filepath.Join(parts...)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var testRates = map[string]float64{
	"2026-10-01": 62.799,
	"2026-10-02": 62.597,
	"2026-10-05": 62.673, // Monday: weekend has no rows
}

func seed(t *testing.T, db *sql.DB, rates map[string]float64) {
	t.Helper()
	if rates == nil {
		rates = testRates
	}
	if _, err := UpsertRates(db, rates, FXBase, FXQuote, FXSource); err != nil {
		t.Fatal(err)
	}
}

func approx(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", what, want)
	}
	diff := *got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Fatalf("%s = %v, want %v", what, *got, want)
	}
}

func TestFXTableCreatedOnConnect(t *testing.T) {
	db := connectTmp(t)
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == "fx_rates" {
			found = true
		}
	}
	if !found {
		t.Fatal("fx_rates table missing")
	}
}

func TestFXAsofWeekendUsesFriday(t *testing.T) {
	db := connectTmp(t)
	seed(t, db, nil)
	approx(t, "10-01", mustAsof(t, db, "2026-10-01"), 62.799)
	approx(t, "10-03 sat", mustAsof(t, db, "2026-10-03"), 62.597)
	approx(t, "10-04 sun", mustAsof(t, db, "2026-10-04"), 62.597)
	approx(t, "10-05", mustAsof(t, db, "2026-10-05"), 62.673)
	if got, err := FXAsOf(db, "2026-09-30", FXBase, FXQuote); err != nil || got != nil {
		t.Fatalf("before-first = %v, %v; want nil", got, err)
	}
}

func mustAsof(t *testing.T, db *sql.DB, date string) *float64 {
	t.Helper()
	got, err := FXAsOf(db, date, FXBase, FXQuote)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestFXUpsertIdempotent(t *testing.T) {
	db := connectTmp(t)
	n1, err := UpsertRates(db, testRates, FXBase, FXQuote, FXSource)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := UpsertRates(db, testRates, FXBase, FXQuote, FXSource)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 3 || n2 != 3 {
		t.Fatalf("counts = %d, %d", n1, n2)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM fx_rates").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("rows = %d", n)
	}
	var rate float64
	var source string
	if err := db.QueryRow("SELECT rate, source FROM fx_rates WHERE date = '2026-10-01'").Scan(&rate, &source); err != nil {
		t.Fatal(err)
	}
	if rate != 62.799 || source != FXSource {
		t.Fatalf("row = (%v, %q)", rate, source)
	}
}

// stubTransport replaces HTTP with a canned fixture payload (mirrors the
// monkeypatched requests.get in test_fx.py).
type stubTransport struct {
	t       *testing.T
	payload map[string]any
	calls   *[]stubCall
}

type stubCall struct {
	url     string
	params  map[string]string
	headers http.Header
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	q := req.URL.Query()
	params := map[string]string{}
	for k, v := range q {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	*s.calls = append(*s.calls, stubCall{url: req.URL.Scheme + "://" + req.URL.Host + req.URL.Path, params: params, headers: req.Header})
	body, _ := json.Marshal(s.payload)
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
	return resp, nil
}

func withStubFx(t *testing.T, payload map[string]any) *[]stubCall {
	t.Helper()
	calls := &[]stubCall{}
	old := Client
	Client = &http.Client{Transport: &stubTransport{t: t, payload: payload, calls: calls}}
	t.Cleanup(func() { Client = old })
	return calls
}

func TestFetchRatesURLAndParams(t *testing.T) {
	const wantUA = "PresyoBantay/0.1 (non-profit community price watch; https://github.com/tomohiro-owada/presyo-bantay)"
	if UserAgent != wantUA {
		t.Fatalf("UserAgent = %q, want %q", UserAgent, wantUA)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/2026-10-01..2026-10-23" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("from") != "USD" || r.URL.Query().Get("to") != "PHP" {
			t.Errorf("params = %q", r.URL.RawQuery)
		}
		if ua := r.Header.Get("User-Agent"); ua != wantUA {
			t.Errorf("UA = %q, want %q", ua, wantUA)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixtureBytes(t, "fx", "frankfurter-usd-php-2026-10.json"))
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL + "/v1"
	defer func() { APIBase = old }()

	rates, err := FetchRates("2026-10-01", "2026-10-23", FXBase, FXQuote)
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 17 {
		t.Fatalf("rates = %d, want 17", len(rates))
	}
	if rates["2026-10-01"] != 62.799 {
		t.Fatalf("10-01 = %v", rates["2026-10-01"])
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSplitYearly(t *testing.T) {
	chunks := SplitYearly("2024-06-01", "2026-02-01")
	want := [][2]string{
		{"2024-06-01", "2024-12-31"},
		{"2025-01-01", "2025-12-31"},
		{"2026-01-01", "2026-02-01"},
	}
	if len(chunks) != len(want) {
		t.Fatalf("chunks = %v", chunks)
	}
	for i := range want {
		if chunks[i] != want[i] {
			t.Fatalf("chunks = %v, want %v", chunks, want)
		}
	}
	if got := SplitYearly("2026-10-01", "2026-10-23"); len(got) != 1 || got[0] != [2]string{"2026-10-01", "2026-10-23"} {
		t.Fatalf("single = %v", got)
	}
}

func TestDefaultSince(t *testing.T) {
	db := connectTmp(t)
	seed(t, db, nil) // latest stored: 2026-10-05
	since, err := DefaultSince(db)
	if err != nil {
		t.Fatal(err)
	}
	if since != "2026-10-06" {
		t.Fatalf("since = %q", since)
	}

	db2 := connectTmp(t)
	if _, err := UpsertRates(db2, map[string]float64{"2026-10-16": 62.968}, FXBase, FXQuote, FXSource); err != nil {
		t.Fatal(err)
	}
	_ = db2
	// earliest price date fallback
	db3 := connectTmp(t)
	payload := `{"date":"2026-10-16","source_url":null,"markets":{"M1":{"Bangus":{"spec":null,"unit":"per kg","price":1.0}}}}`
	_ = payload
	since, err = DefaultSince(db3)
	if err != nil {
		t.Fatal(err)
	}
	if since != "" {
		t.Fatalf("empty db since = %q, want empty", since)
	}
}

func TestUpdateFlowWithStub(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal(fixtureBytes(t, "fx", "frankfurter-usd-php-2026-10.json"), &payload); err != nil {
		t.Fatal(err)
	}
	calls := withStubFx(t, payload)
	db := connectTmp(t)
	since, until := "2026-10-01", "2026-10-23"
	total := 0
	for i, chunk := range SplitYearly(since, until) {
		if i > 0 {
			Sleep(ChunkDelay)
		}
		rates, err := FetchRates(chunk[0], chunk[1], FXBase, FXQuote)
		if err != nil {
			t.Fatal(err)
		}
		n, err := UpsertRates(db, rates, FXBase, FXQuote, FXSource)
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %d", len(*calls))
	}
	if (*calls)[0].url != "https://api.frankfurter.dev/v1/2026-10-01..2026-10-23" {
		t.Fatalf("url = %q", (*calls)[0].url)
	}
	if (*calls)[0].params["from"] != "USD" || (*calls)[0].params["to"] != "PHP" {
		t.Fatalf("params = %v", (*calls)[0].params)
	}
	if total != 17 {
		t.Fatalf("total = %d", total)
	}
	approx(t, "asof 10-24", mustAsof(t, db, "2026-10-24"), 63.642)
	// idempotent re-upsert
	total2 := 0
	for _, chunk := range SplitYearly(since, until) {
		rates, err := FetchRates(chunk[0], chunk[1], FXBase, FXQuote)
		if err != nil {
			t.Fatal(err)
		}
		n, err := UpsertRates(db, rates, FXBase, FXQuote, FXSource)
		if err != nil {
			t.Fatal(err)
		}
		total2 += n
	}
	if total2 != 17 {
		t.Fatalf("re-upsert total = %d", total2)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM fx_rates").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 17 {
		t.Fatalf("rows = %d", n)
	}
}

func TestManilaTodayFormat(t *testing.T) {
	s := ManilaToday()
	if _, err := time.Parse("2006-01-02", s); err != nil {
		t.Fatalf("bad date %q", s)
	}
}
