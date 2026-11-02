// Package grabfare implements the GRABFARE_SPEC.md dry-run prototype:
// a "Grab fare estimate" card for the fixed Zapote Bridge -> Festival
// Supermall Alabang route. All time handling uses a fixed Asia/Manila
// zone (UTC+8, no tzdata). Pure functions; time is injected.
package grabfare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Manila is Asia/Manila as a fixed zone (UTC+8, no DST, no tzdata).
var Manila = time.FixedZone("Asia/Manila", 8*3600)

// UserAgent mirrors the daily pipeline politeness string.
const UserAgent = "PresyoBantay/0.1 (non-profit community price watch; https://github.com/tomohiro-owada/presyo-bantay)"

// Input mirrors the API JSON. Only the documented fields are used.
type InputDoc struct {
	Route      RouteIn   `json:"route"`
	Pickup     *PickupIn `json:"pickup"`
	Bike       *BikeIn   `json:"bike"`
	Source     string    `json:"source"`
	Currency   string    `json:"currency"`
	ServerTime string    `json:"server_time"`
	Hours      int       `json:"hours"`
	Count      int       `json:"count"`
	Runs       []Run     `json:"runs"`
}

// RouteIn is the input route object.
type RouteIn struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Via   string `json:"via"`
	Label string `json:"label"`
}

// PickupIn is the optional top-level pickup block.
type PickupIn struct {
	Current string        `json:"current"`
	SinceTs string        `json:"since_ts"`
	Since   string        `json:"since"`
	History []PickupEntry `json:"history"`
}

// PickupEntry is one pickup era.
type PickupEntry struct {
	SinceTs string `json:"since_ts"`
	Since   string `json:"since"`
	Pickup  string `json:"pickup"`
}

// Run is one row, newest first.
type Run struct {
	Ts         string     `json:"ts"`
	CapturedAt string     `json:"captured_at"`
	AnalyzedAt *string    `json:"analyzed_at"`
	Status     string     `json:"status"`
	Fare       *Fare      `json:"fare"`
	Traffic    *TrafficIn `json:"traffic"`
	MapImages  []string   `json:"map_images"`
}

// Fare mirrors the fare object. Numbers may be null.
type Fare struct {
	Currency    string     `json:"currency"`
	Standard    *PriceOpt  `json:"standard"`
	Saver       *PriceOpt  `json:"saver"`
	MeteredTaxi *MinMaxOpt `json:"metered_taxi"`
	Express     *MinMaxOpt `json:"express"`
}

// PriceOpt is {price, eta_min}.
type PriceOpt struct {
	Price  *float64 `json:"price"`
	EtaMin *float64 `json:"eta_min"`
}

// MinMaxOpt is {min, max, eta_min}.
type MinMaxOpt struct {
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	EtaMin *float64 `json:"eta_min"`
}

// TrafficIn mirrors the traffic object.
type TrafficIn struct {
	Level     string    `json:"level"`
	Summary   string    `json:"summary"`
	GreenPct  *float64  `json:"green_pct"`
	YellowPct *float64  `json:"yellow_pct"`
	RedPct    *float64  `json:"red_pct"`
	Segments  []Segment `json:"segments"`
}

// Segment is one traffic segment.
type Segment struct {
	Level          string  `json:"level"`
	RedSections    StrList `json:"red_sections"`
	YellowSections StrList `json:"yellow_sections"`
}

// StrList decodes a JSON array keeping only its string elements, so a
// stray non-string entry from the upstream analysis never breaks the card.
type StrList []string

// UnmarshalJSON implements json.Unmarshaler (tolerant).
func (l *StrList) UnmarshalJSON(b []byte) error {
	var raw []any
	if err := json.Unmarshal(b, &raw); err != nil {
		*l = nil
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	*l = out
	return nil
}

// RouteOut is the public route object (no label, no hosts).
type RouteOut struct {
	From string `json:"from"`
	To   string `json:"to"`
	Via  string `json:"via"`
}

// PickupOut is the public pickup object.
type PickupOut struct {
	Name      string `json:"name"`
	Since     string `json:"since"`
	SinceHHMM string `json:"since_hhmm"`
	Previous  string `json:"previous,omitempty"`
}

// FareOut carries raw (unrounded) numbers.
type FareOut struct {
	Min      float64  `json:"min"`
	Max      float64  `json:"max"`
	Standard *float64 `json:"standard"`
}

// TrafficOut is the public traffic object.
type TrafficOut struct {
	Level         string   `json:"level"`
	SummaryEn     string   `json:"summary_en"`
	GreenPct      *float64 `json:"green_pct"`
	YellowPct     *float64 `json:"yellow_pct"`
	RedPct        *float64 `json:"red_pct"`
	WorstSections []string `json:"worst_sections"`
}

// TrendOut carries raw deltas.
type TrendOut struct {
	Direction    string   `json:"direction"`
	DeltaPesos   *float64 `json:"delta_pesos"`
	DeltaPct     *float64 `json:"delta_pct"`
	PreviousAsOf *string  `json:"previous_as_of"`
	PreviousHHMM *string  `json:"previous_hhmm"`
}

// LangText holds one language's card strings.
type LangText struct {
	Title      string `json:"title"`
	Headline   string `json:"headline"`
	Asof       string `json:"asof"`
	Traffic    string `json:"traffic"`
	Trend      string `json:"trend"`
	Note       string `json:"note"`
	PickupNote string `json:"pickup_note,omitempty"`
}

// Texts holds tl/en/ja strings.
type Texts struct {
	Tl LangText `json:"tl"`
	En LangText `json:"en"`
	Ja LangText `json:"ja"`
}

// Output is grab-fare.json (public-safe: no hosts/paths/tokens).
type Output struct {
	GeneratedAt string      `json:"generated_at"`
	Visible     bool        `json:"visible"`
	State       string      `json:"state"`
	Reason      string      `json:"reason"`
	ValidUntil  *string     `json:"valid_until"`
	Route       RouteOut    `json:"route"`
	Pickup      *PickupOut  `json:"pickup,omitempty"`
	Currency    string      `json:"currency"`
	Basis       string      `json:"basis"`
	AsOf        *string     `json:"as_of"`
	AsOfHHMM    *string     `json:"as_of_hhmm"`
	AgeMinutes  *int        `json:"age_minutes"`
	Fallback    bool        `json:"fallback"`
	Fare        *FareOut    `json:"fare"`
	Traffic     *TrafficOut `json:"traffic"`
	Trend       TrendOut    `json:"trend"`
	VsYesterday *TrendOut   `json:"vs_yesterday"`
	Text        Texts       `json:"text"`
	// Hourly: today's/yesterday's readings per Manila hour (site chart).
	Hourly *HourlyOut `json:"hourly,omitempty"`
	// Bike is the optional Move It bike fare block.
	Bike *BikeOut `json:"bike,omitempty"`
	// Focus is set by the card command when the wide Colours/SM Southmall
	// band is part of the image (adds one caption line; never serialised).
	Focus bool `json:"-"`
}

// ParseInput parses the API/input-file JSON. It does not invent fields:
// unknown fields are ignored, documented fields are required loosely
// (missing runs is an error, malformed JSON is an error).
func ParseInput(data []byte) (*InputDoc, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var doc InputDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("bad grab payload: %v", err)
	}
	if doc.Runs == nil {
		return nil, fmt.Errorf("bad grab payload: missing runs")
	}
	return &doc, nil
}

// usable is a run that can supply a headline.
type usable struct {
	run      Run
	captured time.Time
	min      float64
	max      float64
	basis    string
	standard *float64
	era      int
}

// usableHeadline computes the headline fares per the spec.
func usableHeadline(r Run) (min, max float64, basis string, standard *float64, ok bool) {
	min, max, basis, standard, ok = rawHeadline(r)
	if ok && !plausible(min, max) {
		// The upstream fare can come from OCR; an implausible range is
		// treated as unusable rather than shown.
		return 0, 0, "", standard, false
	}
	return min, max, basis, standard, ok
}

// plausible guards against OCR misreads: a positive range, min <= max,
// at most PHP 3,000 and at most a 3x spread.
func plausible(min, max float64) bool {
	return min > 0 && max >= min && max <= 3000 && max <= 3*min
}

func rawHeadline(r Run) (min, max float64, basis string, standard *float64, ok bool) {
	if r.Fare == nil {
		return 0, 0, "", nil, false
	}
	if r.Fare.Standard != nil {
		standard = r.Fare.Standard.Price
	}
	mt := r.Fare.MeteredTaxi
	if mt != nil && (mt.Min != nil || mt.Max != nil) {
		lo := mt.Min
		if lo == nil {
			lo = mt.Max
		}
		hi := mt.Max
		if hi == nil {
			hi = mt.Min
		}
		return *lo, *hi, "metered_taxi", standard, true
	}
	if standard != nil {
		return *standard, *standard, "standard", standard, true
	}
	return 0, 0, "", standard, false
}

func parseCaptured(s string) (time.Time, error) {
	// captured_at is RFC3339 with +08:00.
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// trafficOut builds the public traffic object from the same run only.
// TrafficSane reports whether a traffic analysis is usable for the card.
func TrafficSane(t *TrafficIn) (bool, string) {
	if t == nil {
		return false, "empty"
	}
	summary := strings.ToLower(strings.TrimSpace(t.Summary))
	if summary == "" {
		return false, "empty"
	}
	for _, bad := range []string{"notification", "overlay", "screenshot", "supplied", "food-order", "food order", "grab food"} {
		if strings.Contains(summary, bad) {
			return false, "overlay"
		}
	}
	for _, bad := range []string{"unable", "cannot", "can't", "not a map", "no route", "does not show", "doesn't show", "not visible"} {
		if strings.Contains(summary, bad) {
			return false, "unusable"
		}
	}
	level := strings.ToLower(strings.TrimSpace(t.Level))
	switch level {
	case "light", "moderate", "heavy", "severe":
	default:
		return false, "level"
	}
	pcts := []*float64{t.GreenPct, t.YellowPct, t.RedPct}
	for _, p := range pcts {
		if p == nil {
			return false, "pct"
		}
		if *p < 0 || *p > 100 {
			return false, "pct"
		}
	}
	sum := *t.GreenPct + *t.YellowPct + *t.RedPct
	if sum < 95 || sum > 105 {
		return false, "pct"
	}
	return true, ""
}

func trafficOut(t *TrafficIn) *TrafficOut {
	if t == nil || strings.TrimSpace(t.Level) == "" {
		return nil
	}
	if ok, _ := TrafficSane(t); !ok {
		return nil
	}
	seen := map[string]bool{}
	var worst []string
	add := func(names []string) {
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			if len(worst) < 3 {
				worst = append(worst, n)
			}
		}
	}
	// Red sections first in segment order, then yellow.
	for _, s := range t.Segments {
		add(s.RedSections)
	}
	for _, s := range t.Segments {
		add(s.YellowSections)
	}
	if worst == nil {
		worst = []string{}
	}
	return &TrafficOut{
		Level:         t.Level,
		SummaryEn:     t.Summary,
		GreenPct:      t.GreenPct,
		YellowPct:     t.YellowPct,
		RedPct:        t.RedPct,
		WorstSections: worst,
	}
}

// commaInts formats an int64 with thousands separators.
func commaInts(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// FormatPeso formats whole pesos with thousands separator: P282, P1,234.
func FormatPeso(x float64) string {
	return "\u20b1" + commaInts(int64(math.Round(x)))
}

// FormatRange formats min..max: P282–338 (en dash, peso sign once) or
// P282 when equal (comparison on rounded whole pesos).
func FormatRange(min, max float64) string {
	lo := int64(math.Round(min))
	hi := int64(math.Round(max))
	if lo == hi {
		return FormatPeso(min)
	}
	return "\u20b1" + commaInts(lo) + "\u2013" + commaInts(hi)
}

func fmtPct(x float64) string {
	return fmt.Sprintf("%d", int64(math.Round(x)))
}

func midpoint(min, max float64) float64 { return (min + max) / 2 }

func strPtr(s string) *string   { v := s; return &v }
func f64Ptr(f float64) *float64 { v := f; return &v }
func intPtr(i int) *int         { v := i; return &v }

func rfc3339Manila(t time.Time) string { return t.In(Manila).Format(time.RFC3339) }
func hhmmManila(t time.Time) string    { return t.In(Manila).Format("15:04") }

// level words.
var levelTl = map[string]string{
	"light":    "magaan",
	"moderate": "katamtaman",
	"heavy":    "mabigat",
	"severe":   "sobrang bigat",
}

var levelJa = map[string]string{
	"light":    "空いている",
	"moderate": "やや混雑",
	"heavy":    "混雑",
	"severe":   "ひどい渋滞",
}

func tlLevel(l string) string {
	if v, ok := levelTl[l]; ok {
		return v
	}
	return l
}

func jaLevel(l string) string {
	if v, ok := levelJa[l]; ok {
		return v
	}
	return l
}

// headline texts per language.
func headlineEn(min, max float64, basis string) string {
	if basis == "standard" {
		return FormatRange(min, max) + " Grab standard quote"
	}
	return FormatRange(min, max) + " metered taxi quote"
}

func headlineTl(min, max float64, basis string) string {
	if basis == "standard" {
		return FormatRange(min, max) + " tantiya (Grab standard)"
	}
	return FormatRange(min, max) + " tantiya sa metered taxi"
}

func headlineJa(min, max float64, basis string) string {
	if basis == "standard" {
		return FormatRange(min, max) + " スタンダードの目安"
	}
	return FormatRange(min, max) + " メータータクシーの目安"
}

func asofEn(hhmm string, fallback bool) string {
	s := "As of " + hhmm + " (Manila time)"
	if fallback {
		s += " (latest analysed reading)"
	}
	return s
}

func asofTl(hhmm string, fallback bool) string {
	s := "Ayon sa Grab app noong " + hhmm + " (oras sa Maynila)"
	if fallback {
		s += " (huling na-analyze na reading)"
	}
	return s
}

func asofJa(hhmm string, fallback bool) string {
	s := hhmm + "時点のGrabアプリ表示（マニラ時間）"
	if fallback {
		s += "（最新の分析済み）"
	}
	return s
}

func trafficEn(t *TrafficOut) string {
	if t == nil {
		return ""
	}
	if t.RedPct != nil {
		return "Traffic: " + t.Level + " (about " + fmtPct(*t.RedPct) + "% red)"
	}
	return "Traffic: " + t.Level
}

func trafficTl(t *TrafficOut) string {
	if t == nil {
		return ""
	}
	if t.RedPct != nil {
		return "Trapiko: " + tlLevel(t.Level) + " (mga " + fmtPct(*t.RedPct) + "% na pula)"
	}
	return "Trapiko: " + tlLevel(t.Level)
}

func trafficJa(t *TrafficOut) string {
	if t == nil {
		return ""
	}
	if t.RedPct != nil {
		return "交通状況：" + jaLevel(t.Level) + "（赤色約" + fmtPct(*t.RedPct) + "%）"
	}
	return "交通状況：" + jaLevel(t.Level)
}

// trendText renders the trend line per language. prevHHMM is "" when none.
func trendTextEn(d TrendOut) string {
	switch d.Direction {
	case "up":
		amt := ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		pct := ""
		if d.DeltaPct != nil {
			pct = "(+" + fmtPct(*d.DeltaPct) + "%)"
		}
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " vs " + *d.PreviousHHMM
		}
		return strings.TrimSpace("\u25b2 " + amt + " " + pct + prev)
	case "down":
		amt := ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		pct := ""
		if d.DeltaPct != nil {
			pct = "(-" + fmtPct(math.Abs(*d.DeltaPct)) + "%)"
		}
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " vs " + *d.PreviousHHMM
		}
		return strings.TrimSpace("\u25bc " + amt + " " + pct + prev)
	case "flat":
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " vs " + *d.PreviousHHMM
		}
		return "\uff1d No change" + prev
	default:
		return "No earlier reading yet"
	}
}

func trendTextTl(d TrendOut) string {
	switch d.Direction {
	case "up", "down":
		arrow := "\u25b2"
		if d.Direction == "down" {
			arrow = "\u25bc"
		}
		amt := ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		pct := ""
		if d.DeltaPct != nil {
			if d.Direction == "up" {
				pct = "(+" + fmtPct(*d.DeltaPct) + "%)"
			} else {
				pct = "(-" + fmtPct(math.Abs(*d.DeltaPct)) + "%)"
			}
		}
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " kumpara sa " + *d.PreviousHHMM
		}
		return strings.TrimSpace(arrow + " " + amt + " " + pct + prev)
	case "flat":
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " kumpara sa " + *d.PreviousHHMM
		}
		return "\uff1d Walang pagbabago" + prev
	default:
		return "Wala pang naunang reading"
	}
}

func trendTextJa(d TrendOut) string {
	switch d.Direction {
	case "up", "down":
		arrow := "\u25b2"
		if d.Direction == "down" {
			arrow = "\u25bc"
		}
		amt := ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		pct := ""
		if d.DeltaPct != nil {
			if d.Direction == "up" {
				pct = "（+" + fmtPct(*d.DeltaPct) + "%）"
			} else {
				pct = "（-" + fmtPct(math.Abs(*d.DeltaPct)) + "%）"
			}
		}
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " " + *d.PreviousHHMM + "比"
		}
		return strings.TrimSpace(arrow + " " + amt + pct + prev)
	case "flat":
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " " + *d.PreviousHHMM + "比で変化なし"
		} else {
			prev = " 変化なし"
		}
		return "\uff1d" + prev
	default:
		return "以前の記録はまだありません"
	}
}

const (
	noteEn = "Quote shown in the Grab app at that time. Not an official fare; actual fare may differ."
	noteTl = "Quote lang ito mula sa Grab app sa oras na iyon. Hindi opisyal na pamasahe."
	noteJa = "その時点でGrabアプリに表示された料金です。公式運賃ではありません。"
)

// makeTrend builds a TrendOut comparing cur to prev.
func makeTrend(cur, prev usable) TrendOut {
	curMid := midpoint(cur.min, cur.max)
	prevMid := midpoint(prev.min, prev.max)
	delta := curMid - prevMid
	var pct *float64
	if prevMid != 0 {
		p := delta / prevMid * 100
		pct = &p
	}
	dir := "flat"
	switch {
	case delta >= 1:
		dir = "up"
	case delta <= -1:
		dir = "down"
	}
	asof := rfc3339Manila(prev.captured)
	hhmm := hhmmManila(prev.captured)
	d := delta
	return TrendOut{
		Direction:    dir,
		DeltaPesos:   &d,
		DeltaPct:     pct,
		PreviousAsOf: &asof,
		PreviousHHMM: &hhmm,
	}
}

func noneTrend() TrendOut {
	return TrendOut{Direction: "none"}
}

// pickupValid reports whether doc has a usable pickup block.
func pickupValid(doc *InputDoc) bool {
	if doc == nil || doc.Pickup == nil {
		return false
	}
	return strings.TrimSpace(doc.Pickup.Current) != "" && strings.TrimSpace(doc.Pickup.SinceTs) != ""
}

// era implements the Era helper on InputDoc.
func (d *InputDoc) era(ts string) int {
	if !pickupValid(d) {
		return 0
	}
	n := 0
	for _, h := range d.Pickup.History {
		if strings.Compare(h.SinceTs, ts) > 0 {
			n++
		}
	}
	return n
}

// pickupPrevious returns the name of the pickup before the current one
// (the history entry just before the one that starts at pickup.since_ts).
func pickupPrevious(doc *InputDoc) string {
	if !pickupValid(doc) {
		return ""
	}
	hist := doc.Pickup.History
	for i := range hist {
		if hist[i].SinceTs == doc.Pickup.SinceTs {
			if i > 0 {
				return hist[i-1].Pickup
			}
			return ""
		}
	}
	return ""
}

// pickupSinceTime parses the pickup since into a Manila time.
func pickupSinceTime(doc *InputDoc) time.Time {
	if !pickupValid(doc) {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, doc.Pickup.Since); err == nil {
		return t.In(Manila)
	}
	if t, err := time.Parse("20060102-1504", doc.Pickup.SinceTs); err == nil {
		return t.In(Manila)
	}
	return time.Time{}
}

// pickupSinceHHMM returns HH:MM for the pickup change.
func pickupSinceHHMM(doc *InputDoc) string {
	if !pickupValid(doc) {
		return ""
	}
	if doc.Pickup.Since != "" {
		if t, err := time.Parse(time.RFC3339, doc.Pickup.Since); err == nil {
			return t.In(Manila).Format("15:04")
		}
	}
	ts := doc.Pickup.SinceTs
	if len(ts) >= 13 {
		return ts[9:11] + ":" + ts[11:13]
	}
	return ""
}

// pickupSinceDate returns the Manila date (YYYY-MM-DD) of the pickup change.
func pickupSinceDate(doc *InputDoc) string {
	t := pickupSinceTime(doc)
	if !t.IsZero() {
		return t.Format("2006-01-02")
	}
	ts := doc.Pickup.SinceTs
	if len(ts) >= 8 {
		return ts[0:4] + "-" + ts[4:6] + "-" + ts[6:8]
	}
	return ""
}

// pickupOut builds the public pickup object when valid.
func pickupOut(doc *InputDoc) *PickupOut {
	if !pickupValid(doc) {
		return nil
	}
	since := doc.Pickup.Since
	if since == "" {
		t := pickupSinceTime(doc)
		if !t.IsZero() {
			since = t.Format(time.RFC3339)
		}
	}
	return &PickupOut{
		Name:      doc.Pickup.Current,
		Since:     since,
		SinceHHMM: pickupSinceHHMM(doc),
		Previous:  pickupPrevious(doc),
	}
}

// pickupNoteRelevant reports whether the pickup note should be shown.
func pickupNoteRelevant(doc *InputDoc, now time.Time) bool {
	if !pickupValid(doc) {
		return false
	}
	if pickupPrevious(doc) == "" {
		return false
	}
	sinceDate := pickupSinceDate(doc)
	yesterday := now.In(Manila).AddDate(0, 0, -1).Format("2006-01-02")
	return sinceDate >= yesterday
}

// Build computes the public card. now is the injected clock.
func Build(doc *InputDoc, now time.Time, staleAfter time.Duration) *Output {
	gen := rfc3339Manila(now)
	currency := doc.Currency
	if currency == "" {
		currency = "PHP"
	}
	route := RouteOut{From: doc.Route.From, To: doc.Route.To, Via: doc.Route.Via}
	if pickupValid(doc) {
		route.From = doc.Pickup.Current
	}

	type ordered struct {
		run      Run
		captured time.Time
		okTime   bool
	}
	var all []ordered
	for _, r := range doc.Runs {
		t, err := parseCaptured(r.CapturedAt)
		if err != nil {
			continue
		}
		all = append(all, ordered{run: r, captured: t, okTime: true})
	}
	// Newest first by captured_at.
	sort.Slice(all, func(i, j int) bool { return all[i].captured.After(all[j].captured) })

	var usables []usable
	for _, o := range all {
		min, max, basis, std, ok := usableHeadline(o.run)
		if !ok {
			continue
		}
		usables = append(usables, usable{run: o.run, captured: o.captured, min: min, max: max, basis: basis, standard: std, era: doc.era(o.run.Ts)})
	}

	var curUsables []usable
	for _, u := range usables {
		if u.era == 0 {
			curUsables = append(curUsables, u)
		}
	}

	hiddenTexts := func() Texts {
		return Texts{
			En: LangText{Title: "Grab fare estimate", Note: noteEn},
			Tl: LangText{Title: "Tantiya sa pamasahe ng Grab", Note: noteTl},
			Ja: LangText{Title: "Grab料金の目安", Note: noteJa},
		}
	}

	if len(curUsables) == 0 {
		return &Output{
			GeneratedAt: gen,
			Visible:     false,
			State:       "unavailable",
			Reason:      "unavailable: no usable fare reading",
			Route:       route,
			Pickup:      pickupOut(doc),
			Currency:    currency,
			Trend:       noneTrend(),
			Text:        hiddenTexts(),
		}
	}

	cur := curUsables[0]
	isPendingNewest := len(all) > 0 && all[0].run.Status == "pending"
	fallback := isPendingNewest && !all[0].captured.Equal(cur.captured)
	age := now.Sub(cur.captured)
	ageMin := int(math.Floor(age.Minutes()))
	if ageMin < 0 {
		ageMin = 0
	}
	asof := rfc3339Manila(cur.captured)
	hhmm := hhmmManila(cur.captured)
	valid := rfc3339Manila(cur.captured.Add(staleAfter))

	if age > staleAfter {
		// Stale: hidden, but carry the stale reading's times for audit.
		return &Output{
			GeneratedAt: gen,
			Visible:     false,
			State:       "stale",
			Reason:      "stale: latest usable reading is older than the freshness window",
			ValidUntil:  strPtr(valid),
			Route:       route,
			Pickup:      pickupOut(doc),
			Currency:    currency,
			Basis:       cur.basis,
			AsOf:        strPtr(asof),
			AsOfHHMM:    strPtr(hhmm),
			AgeMinutes:  intPtr(ageMin),
			Fallback:    fallback,
			Fare:        &FareOut{Min: cur.min, Max: cur.max, Standard: cur.standard},
			Traffic:     trafficOut(cur.run.Traffic),
			Trend:       trendFor(cur, curUsables),
			Text:        hiddenTexts(),
		}
	}

	state := "fresh"
	reason := "fresh"
	if fallback {
		state = "fallback"
		reason = "fallback: latest run is pending, showing latest analysed reading"
	}

	tr := trendFor(cur, curUsables)
	vy := vsYesterday(cur, curUsables)
	tout := trafficOut(cur.run.Traffic)
	fareOut := &FareOut{Min: cur.min, Max: cur.max, Standard: cur.standard}
	pickupNoteTl := ""
	pickupNoteEn := ""
	pickupNoteJa := ""
	if pickupNoteRelevant(doc, now) {
		n := pickupSinceHHMM(doc)
		p := doc.Pickup.Current
		q := pickupPrevious(doc)
		pickupNoteTl = "Bagong sundo mula " + n + ": " + p + ". Hindi kasama sa paghahambing ang mga naunang presyo mula sa " + q + "."
		pickupNoteEn = "Pickup changed at " + n + " to " + p + ". Earlier fares from " + q + " are not compared."
		pickupNoteJa = n + "から乗車地点を" + p + "に変更。以前の" + q + "の料金とは比較していません。"
	}
	texts := Texts{
		En: LangText{
			Title:      "Grab fare estimate",
			Headline:   headlineEn(cur.min, cur.max, cur.basis),
			Asof:       asofEn(hhmm, fallback),
			Traffic:    trafficEn(tout),
			Trend:      trendTextEn(tr),
			Note:       noteEn,
			PickupNote: pickupNoteEn,
		},
		Tl: LangText{
			Title:      "Tantiya sa pamasahe ng Grab",
			Headline:   headlineTl(cur.min, cur.max, cur.basis),
			Asof:       asofTl(hhmm, fallback),
			Traffic:    trafficTl(tout),
			Trend:      trendTextTl(tr),
			Note:       noteTl,
			PickupNote: pickupNoteTl,
		},
		Ja: LangText{
			Title:      "Grab料金の目安",
			Headline:   headlineJa(cur.min, cur.max, cur.basis),
			Asof:       asofJa(hhmm, fallback),
			Traffic:    trafficJa(tout),
			Trend:      trendTextJa(tr),
			Note:       noteJa,
			PickupNote: pickupNoteJa,
		},
	}
	return &Output{
		GeneratedAt: gen,
		Visible:     true,
		State:       state,
		Reason:      reason,
		ValidUntil:  strPtr(valid),
		Route:       route,
		Currency:    currency,
		Basis:       cur.basis,
		AsOf:        strPtr(asof),
		AsOfHHMM:    strPtr(hhmm),
		AgeMinutes:  intPtr(ageMin),
		Fallback:    fallback,
		Fare:        fareOut,
		Traffic:     tout,
		Trend:       tr,
		VsYesterday: vy,
		Text:        texts,
		Hourly:      buildHourly(usables, now),
		Bike:        buildBike(doc, now),
		Pickup:      pickupOut(doc),
	}
}

// BuildFromRun is like Build but treats the run with the given ts as the
// newest (runs captured after it are dropped). Returns a hidden/unavailable
// output when that run is not usable.
func BuildFromRun(doc *InputDoc, now time.Time, staleAfter time.Duration, ts string) *Output {
	if doc != nil && doc.era(ts) > 0 {
		return unavailableOutput(doc, now)
	}
	var target *Run
	var targetCaptured time.Time
	found := false
	for i := range doc.Runs {
		if doc.Runs[i].Ts != ts {
			continue
		}
		t, err := parseCaptured(doc.Runs[i].CapturedAt)
		if err != nil {
			// Unusable captured time -> unavailable.
			return unavailableOutput(doc, now)
		}
		r := doc.Runs[i]
		target = &r
		targetCaptured = t
		found = true
		break
	}
	if !found || target == nil {
		return unavailableOutput(doc, now)
	}
	if _, _, _, _, ok := usableHeadline(*target); !ok {
		// Force unavailable by considering only the target run.
		clone := *doc
		clone.Runs = []Run{*target}
		return Build(&clone, now, staleAfter)
	}
	clone := *doc
	var runs []Run
	for _, r := range doc.Runs {
		t, err := parseCaptured(r.CapturedAt)
		if err != nil {
			continue
		}
		if t.After(targetCaptured) {
			continue
		}
		runs = append(runs, r)
	}
	clone.Runs = runs
	return Build(&clone, now, staleAfter)
}

func unavailableOutput(doc *InputDoc, now time.Time) *Output {
	currency := ""
	route := RouteOut{}
	if doc != nil {
		currency = doc.Currency
		route = RouteOut{From: doc.Route.From, To: doc.Route.To, Via: doc.Route.Via}
		if pickupValid(doc) {
			route.From = doc.Pickup.Current
		}
	}
	if currency == "" {
		currency = "PHP"
	}
	return &Output{
		GeneratedAt: rfc3339Manila(now),
		Visible:     false,
		State:       "unavailable",
		Reason:      "unavailable: no usable fare reading",
		Route:       route,
		Pickup:      pickupOut(doc),
		Currency:    currency,
		Trend:       noneTrend(),
		Text: Texts{
			En: LangText{Title: "Grab fare estimate", Note: noteEn},
			Tl: LangText{Title: "Tantiya sa pamasahe ng Grab", Note: noteTl},
			Ja: LangText{Title: "Grab料金の目安", Note: noteJa},
		},
	}
}

// trendFor finds the next older usable run within the 26h window.
func trendFor(cur usable, usables []usable) TrendOut {
	for _, u := range usables {
		if !u.captured.Before(cur.captured) {
			continue
		}
		if cur.captured.Sub(u.captured) <= 26*time.Hour {
			return makeTrend(cur, u)
		}
		// Usables are newest-first, so the first older one is the
		// previous; if it is outside the window there is no trend.
		break
	}
	return noneTrend()
}

// vsYesterday finds a usable run 24h +/- 30min before current.
func vsYesterday(cur usable, usables []usable) *TrendOut {
	var best *usable
	bestDiff := time.Duration(1<<62 - 1)
	target := 24 * time.Hour
	for i := range usables {
		u := usables[i]
		if !u.captured.Before(cur.captured) {
			continue
		}
		d := cur.captured.Sub(u.captured)
		if d < 23*time.Hour+30*time.Minute || d > 24*time.Hour+30*time.Minute {
			continue
		}
		dd := d - target
		if dd < 0 {
			dd = -dd
		}
		if dd < bestDiff {
			c := u
			best = &c
			bestDiff = dd
		}
	}
	if best == nil {
		return nil
	}
	t := makeTrend(cur, *best)
	return &t
}

// TrendTextEn/Tl/Ja are exported wrappers around the trend text helpers.
func TrendTextEn(d TrendOut) string { return trendTextEn(d) }
func TrendTextTl(d TrendOut) string { return trendTextTl(d) }
func TrendTextJa(d TrendOut) string { return trendTextJa(d) }

// YesterdayTextEn/Tl/Ja format a vs-yesterday trend.
func YesterdayTextEn(d TrendOut) string {
	switch d.Direction {
	case "up":
		amt, pct, prev := "", "", ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		if d.DeltaPct != nil {
			pct = "(+" + fmtPct(*d.DeltaPct) + "%)"
		}
		if d.PreviousHHMM != nil {
			prev = " vs yesterday " + *d.PreviousHHMM
		}
		return strings.TrimSpace("\u25b2 " + amt + " " + pct + prev)
	case "down":
		amt, pct, prev := "", "", ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		if d.DeltaPct != nil {
			pct = "(-" + fmtPct(math.Abs(*d.DeltaPct)) + "%)"
		}
		if d.PreviousHHMM != nil {
			prev = " vs yesterday " + *d.PreviousHHMM
		}
		return strings.TrimSpace("\u25bc " + amt + " " + pct + prev)
	case "flat":
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " vs yesterday " + *d.PreviousHHMM
		}
		return "\uff1d No change" + prev
	default:
		return "No earlier reading yet"
	}
}

func YesterdayTextTl(d TrendOut) string {
	switch d.Direction {
	case "up", "down":
		arrow := "\u25b2"
		if d.Direction == "down" {
			arrow = "\u25bc"
		}
		amt, pct, prev := "", "", ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		if d.DeltaPct != nil {
			if d.Direction == "up" {
				pct = "(+" + fmtPct(*d.DeltaPct) + "%)"
			} else {
				pct = "(-" + fmtPct(math.Abs(*d.DeltaPct)) + "%)"
			}
		}
		if d.PreviousHHMM != nil {
			prev = " kumpara kahapon " + *d.PreviousHHMM
		}
		return strings.TrimSpace(arrow + " " + amt + " " + pct + prev)
	case "flat":
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " kumpara kahapon " + *d.PreviousHHMM
		}
		return "\uff1d Walang pagbabago" + prev
	default:
		return "Wala pang naunang reading"
	}
}

func YesterdayTextJa(d TrendOut) string {
	switch d.Direction {
	case "up", "down":
		arrow := "\u25b2"
		if d.Direction == "down" {
			arrow = "\u25bc"
		}
		amt, pct, prev := "", "", ""
		if d.DeltaPesos != nil {
			amt = FormatPeso(math.Abs(*d.DeltaPesos))
		}
		if d.DeltaPct != nil {
			if d.Direction == "up" {
				pct = "（+" + fmtPct(*d.DeltaPct) + "%）"
			} else {
				pct = "（-" + fmtPct(math.Abs(*d.DeltaPct)) + "%）"
			}
		}
		if d.PreviousHHMM != nil {
			prev = " 昨日" + *d.PreviousHHMM + "比"
		}
		return strings.TrimSpace(arrow + " " + amt + pct + prev)
	case "flat":
		prev := ""
		if d.PreviousHHMM != nil {
			prev = " 昨日" + *d.PreviousHHMM + "比で変化なし"
		} else {
			prev = " 変化なし"
		}
		return "\uff1d" + prev
	default:
		return "以前の記録はまだありません"
	}
}

// MarshalJSONDoc encodes the output with indent 2 and a trailing newline,
// matching repo JSON conventions.
func MarshalJSONDoc(o *Output) []byte {
	data, _ := json.MarshalIndent(o, "", "  ")
	return append(data, '\n')
}

// Candidate is a run eligible for the grab card.
type Candidate struct {
	Run      Run
	Ts       string
	Captured time.Time
}

// SelectCardRun returns candidates newest-first: usable fare, young enough,
// with at least one cropped map image.
func SelectCardRun(doc *InputDoc, now time.Time, maxAge time.Duration) []Candidate {
	return SelectRuns(doc, now, maxAge, true)
}

// SelectRuns is SelectCardRun with the map requirement switchable (fare-only
// cards pass requireMaps=false).
func SelectRuns(doc *InputDoc, now time.Time, maxAge time.Duration, requireMaps bool) []Candidate {
	var all []struct {
		run      Run
		captured time.Time
	}
	for _, r := range doc.Runs {
		t, err := parseCaptured(r.CapturedAt)
		if err != nil {
			continue
		}
		all = append(all, struct {
			run      Run
			captured time.Time
		}{run: r, captured: t})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].captured.After(all[j].captured) })

	var out []Candidate
	for _, o := range all {
		if doc != nil && doc.era(o.run.Ts) > 0 {
			continue
		}
		if _, _, _, _, ok := usableHeadline(o.run); !ok {
			continue
		}
		if now.Sub(o.captured) > maxAge {
			continue
		}
		if requireMaps && len(o.run.MapImages) == 0 {
			continue
		}
		if requireMaps && o.run.Traffic != nil {
			ok, reason := TrafficSane(o.run.Traffic)
			if !ok && reason == "overlay" {
				continue
			}
		}
		out = append(out, Candidate{Run: o.run, Ts: o.run.Ts, Captured: o.captured})
	}
	if out == nil {
		out = []Candidate{}
	}
	return out
}

// routeLine renders "from → to (via)".
func routeLine(r RouteOut) string {
	if strings.TrimSpace(r.Via) != "" {
		return r.From + " \u2192 " + r.To + " (" + r.Via + ")"
	}
	if strings.TrimSpace(r.From) == "" && strings.TrimSpace(r.To) == "" {
		return ""
	}
	return r.From + " \u2192 " + r.To
}

const snippetCSS = `<style>.grab-fare{background:var(--card,#fff);color:var(--ink,#1c1a15);border:1px solid var(--line,#e2dccb);border-radius:12px;padding:12px 14px;font-family:sans-serif}.grab-fare .gf-muted{color:var(--muted,#5c5648)}.grab-fare .gf-up{color:var(--up,#b3261e)}.grab-fare .gf-down{color:var(--down,#1e7e34)}.grab-fare .gf-accent{color:var(--accent,#0b5d3b)}</style>`

// RenderSnippet renders grab-fare.html. Hidden states render as an HTML
// comment only.
func RenderSnippet(o *Output) string {
	if !o.Visible {
		if o.State == "unavailable" {
			return "<!-- grab-fare: hidden (unavailable) -->\n"
		}
		return "<!-- grab-fare: hidden (stale) -->\n"
	}
	e := html.EscapeString
	valid := ""
	if o.ValidUntil != nil {
		valid = *o.ValidUntil
	}
	var b strings.Builder
	b.WriteString(snippetCSS + "\n")
	b.WriteString(`<section class="grab-fare" data-valid-until="` + e(valid) + `" aria-label="` + e(o.Text.En.Title) + "\">\n")
	route := e(routeLine(o.Route))
	langs := []struct {
		code string
		t    LangText
		hide bool
	}{
		{"tl", o.Text.Tl, false},
		{"en", o.Text.En, true},
		{"ja", o.Text.Ja, true},
	}
	for _, l := range langs {
		b.WriteString(`<div lang="` + l.code + `" class="gf-lang"`)
		if l.hide {
			b.WriteString(" hidden")
		}
		b.WriteString(">\n")
		b.WriteString("<h2>" + e(l.t.Title) + "</h2>\n")
		if route != "" {
			b.WriteString(`<p class="gf-route">` + route + "</p>\n")
		}
		if l.t.Headline != "" {
			b.WriteString(`<p class="gf-headline"><strong>` + e(l.t.Headline) + "</strong></p>\n")
		}
		if l.t.Asof != "" {
			b.WriteString(`<p class="gf-muted">` + e(l.t.Asof) + "</p>\n")
		}
		if l.t.Traffic != "" {
			b.WriteString(`<p class="gf-traffic">` + e(l.t.Traffic) + "</p>\n")
		}
		if l.t.Trend != "" {
			b.WriteString(`<p class="gf-trend">` + e(l.t.Trend) + "</p>\n")
		}
		if l.t.Note != "" {
			b.WriteString(`<p class="gf-muted">` + e(l.t.Note) + "</p>\n")
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</section>\n")
	return b.String()
}

// RenderPreview renders the standalone review page wrapping the snippet.
func RenderPreview(o *Output, snippet string) string {
	e := html.EscapeString
	title := e(o.Text.En.Title)
	if title == "" {
		title = "Grab fare estimate"
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString("<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n")
	b.WriteString("<title>" + title + " preview</title>\n")
	b.WriteString("<style>:root{--bg:#faf8f2;--card:#fff;--ink:#1c1a15;--muted:#5c5648;--line:#e2dccb;--accent:#0b5d3b;--up:#b3261e;--down:#1e7e34}body{background:var(--bg,#faf8f2);color:var(--ink,#1c1a15);font-family:sans-serif;margin:0;padding:24px}.gf-wrap{max-width:640px;margin:0 auto}.gf-btns{margin:0 0 12px}</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString("<div class=\"gf-wrap\">\n")
	b.WriteString("<div class=\"gf-btns\">\n")
	b.WriteString("<button type=\"button\" data-gflang=\"tl\">TL</button>\n")
	b.WriteString("<button type=\"button\" data-gflang=\"en\">EN</button>\n")
	b.WriteString("<button type=\"button\" data-gflang=\"ja\">JA</button>\n")
	b.WriteString("</div>\n")
	b.WriteString(snippet)
	if !strings.HasSuffix(snippet, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("</div>\n")
	b.WriteString("<script>(function(){var b=document.querySelectorAll('[data-gflang]');function show(l){var ds=document.querySelectorAll('.gf-lang');for(var i=0;i<ds.length;i++){ds[i].hidden=ds[i].getAttribute('lang')!==l;}}for(var i=0;i<b.length;i++){b[i].addEventListener('click',function(){show(this.getAttribute('data-gflang'));});}})();</script>\n")
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// FetchGET performs GET {base}/v1/grab/recent?hours=N with a bearer token.
func FetchGET(base string, hours int, token string, timeout time.Duration) ([]byte, error) {
	u, err := url.Parse(strings.TrimSuffix(base, "/"))
	if err != nil {
		return nil, fmt.Errorf("fetch failed")
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/grab/recent"
	q := u.Query()
	q.Set("hours", fmt.Sprintf("%d", hours))
	u.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch failed")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", UserAgent)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch failed with status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("fetch failed")
	}
	return data, nil
}

// ExpireForFailure implements the fetch/parse failure path: when the
// existing DIR/grab-fare.json carries a valid_until in the past (relative
// to now) it returns a hidden stale output to rewrite; otherwise nil
// (leave files untouched).
func ExpireForFailure(existing []byte, now time.Time) *Output {
	var doc struct {
		ValidUntil *string    `json:"valid_until"`
		Route      RouteOut   `json:"route"`
		Pickup     *PickupOut `json:"pickup"`
		Currency   string     `json:"currency"`
	}
	if err := json.Unmarshal(existing, &doc); err != nil {
		return nil
	}
	if doc.ValidUntil == nil || *doc.ValidUntil == "" {
		return nil
	}
	vu, err := time.Parse(time.RFC3339, *doc.ValidUntil)
	if err != nil {
		return nil
	}
	if !vu.Before(now) {
		return nil
	}
	currency := doc.Currency
	if currency == "" {
		currency = "PHP"
	}
	return &Output{
		GeneratedAt: rfc3339Manila(now),
		Visible:     false,
		State:       "stale",
		Reason:      "stale: source update failed and the previous card expired",
		ValidUntil:  doc.ValidUntil,
		Route:       doc.Route,
		Pickup:      doc.Pickup,
		Currency:    currency,
		Trend:       noneTrend(),
		Text: Texts{
			En: LangText{Title: "Grab fare estimate", Note: noteEn},
			Tl: LangText{Title: "Tantiya sa pamasahe ng Grab", Note: noteTl},
			Ja: LangText{Title: "Grab料金の目安", Note: noteJa},
		},
	}
}
