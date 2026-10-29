package utilpages

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

var pageTmpl = template.Must(template.ParseFS(tmplFS,
	"templates/ucommon.html.tmpl", "templates/upost.html.tmpl", "templates/uindex.html.tmpl"))

// pesoRe matches one peso amount so it can be wrapped in a nowrap span
// (extended units for utility captions: /L, /kWh, /cu.m).
var pesoRe = regexp.MustCompile(`[\(（]?[-‐‑‒–—−+]?₱\d[\d,]*(?:\.\d+)?(?:/(?:kg|piraso|pc|L|kWh|cu\.m))?[\)）]?`)

// Seg is one renderable text run.
type Seg struct {
	Text   string
	Nowrap bool
}

// splitPeso splits s so peso amounts render unbroken.
func splitPeso(s string) []Seg {
	var out []Seg
	last := 0
	for _, loc := range pesoRe.FindAllStringIndex(s, -1) {
		if loc[0] > last {
			out = append(out, Seg{Text: s[last:loc[0]]})
		}
		out = append(out, Seg{Text: s[loc[0]:loc[1]], Nowrap: true})
		last = loc[1]
	}
	if last < len(s) {
		out = append(out, Seg{Text: s[last:]})
	}
	if len(out) == 0 {
		out = append(out, Seg{Text: s})
	}
	return out
}

// Item is one bullet line.
type Item struct {
	Segs []Seg
}

// Block is one rendered text group.
type Block struct {
	Kind  string // "head", "para", "list"
	Segs  []Seg  // head/para
	Items []Item // list
}

// toBlocks converts one language block into render groups: first line
// is the head, "• " lines form lists, the rest are paragraphs.
func toBlocks(text string) []Block {
	var out []Block
	var list []Item
	flush := func() {
		if len(list) > 0 {
			out = append(out, Block{Kind: "list", Items: list})
			list = nil
		}
	}
	headed := false
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if !headed {
			out = append(out, Block{Kind: "head", Segs: splitPeso(t)})
			headed = true
			continue
		}
		if strings.HasPrefix(t, "• ") {
			list = append(list, Item{Segs: splitPeso(strings.TrimSpace(t[4:]))})
			continue
		}
		flush()
		out = append(out, Block{Kind: "para", Segs: splitPeso(t)})
	}
	flush()
	if out == nil {
		out = []Block{}
	}
	return out
}

// dropHead removes a leading head block (the h1 already shows it).
func dropHead(bs []Block) []Block {
	if len(bs) > 0 && bs[0].Kind == "head" {
		return bs[1:]
	}
	return bs
}

// splitCaption splits a rendered caption into TL/EN/JA/footer.
func splitCaption(text string) (tl, en, ja, footer string) {
	parts := strings.Split(text, utilities.Separator)
	get := func(i int) string {
		if i >= len(parts) {
			return ""
		}
		return strings.Trim(parts[i], "\n")
	}
	return get(0), get(1), get(2), get(3)
}

// fuelfmt prints a signed 2-decimal peso value (U+2212 minus).
func fuelfmt(v float64) string {
	if v < 0 {
		return fmt.Sprintf("−₱%.2f", -v)
	}
	return fmt.Sprintf("+₱%.2f", v)
}

// fuelRangeLine formats one adjustment week for the 6-week list.
func fuelRangeLine(a utilities.OilAdjustment) (tl, en, ja string) {
	from, to := a.EffectiveFrom, a.EffectiveTo
	rtl, ren, rja := utilities.ShortRange3(from, to)
	tparts := []string{}
	eparts := []string{}
	jparts := []string{}
	if a.Gasoline != nil {
		g := a.Gasoline
		gs := fuelfmt(g.Lo)
		if g.Lo != g.Hi {
			gs += " hanggang " + fuelfmt(g.Hi)
		}
		tparts = append(tparts, "gasolina "+gs)
		ge := fuelfmt(g.Lo)
		if g.Lo != g.Hi {
			ge += " to " + fuelfmt(g.Hi)
		}
		eparts = append(eparts, "gasoline "+ge)
		gj := fuelfmt(g.Lo)
		if g.Lo != g.Hi {
			gj += "〜" + fuelfmt(g.Hi)
		}
		jparts = append(jparts, "ガソリン "+gj)
	}
	if a.Diesel != nil {
		d := a.Diesel
		ds := fuelfmt(d.Lo)
		if d.Lo != d.Hi {
			ds += " hanggang " + fuelfmt(d.Hi)
		}
		tparts = append(tparts, "diesel "+ds)
		de := fuelfmt(d.Lo)
		if d.Lo != d.Hi {
			de += " to " + fuelfmt(d.Hi)
		}
		eparts = append(eparts, "diesel "+de)
		dj := fuelfmt(d.Lo)
		if d.Lo != d.Hi {
			dj += "〜" + fuelfmt(d.Hi)
		}
		jparts = append(jparts, "軽油 "+dj)
	}
	if a.Kerosene != nil {
		k := a.Kerosene
		ks := fuelfmt(k.Lo)
		if k.Lo != k.Hi {
			ks += " hanggang " + fuelfmt(k.Hi)
		}
		tparts = append(tparts, "kerosene "+ks)
		ke := fuelfmt(k.Lo)
		if k.Lo != k.Hi {
			ke += " to " + fuelfmt(k.Hi)
		}
		eparts = append(eparts, "kerosene "+ke)
		kj := fuelfmt(k.Lo)
		if k.Lo != k.Hi {
			kj += "〜" + fuelfmt(k.Hi)
		}
		jparts = append(jparts, "灯油 "+kj)
	}
	tl = rtl + ": " + strings.Join(tparts, " · ")
	en = ren + ": " + strings.Join(eparts, " · ")
	ja = rja + ": " + strings.Join(jparts, "・")
	return tl, en, ja
}

// PumpCell is one site pump-table cell.
type PumpCell struct {
	Value string
	Delta string
	Up    bool
	None  bool // no data (renders "—")
}

// PumpRow is one site pump-table row.
type PumpRow struct {
	Label string
	Cells []PumpCell // RON91, RON95, Diesel
}

// pumpTable builds the now-vs-previous table rows (NCR + nearby cities
// present this week).
func pumpTable(snap *utilities.Snapshot) (nowRange, prevRange string, rows []PumpRow) {
	now, prev := utilities.TrendTable(snap)
	if now == nil {
		return "", "", nil
	}
	ntl, _, _ := utilities.ShortRange3(now.WeekFrom, now.WeekTo)
	nowRange = ntl
	if prev != nil {
		prtl, _, _ := utilities.ShortRange3(prev.WeekFrom, prev.WeekTo)
		prevRange = prtl
	}
	mkrow := func(label string, get func(product string) (now, prev *float64, present bool)) (PumpRow, bool) {
		row := PumpRow{Label: label}
		any := false
		for _, p := range []string{"RON91", "RON95", "Diesel"} {
			nv, pv, present := get(p)
			if !present {
				row.Cells = append(row.Cells, PumpCell{None: true})
				continue
			}
			any = true
			cell := PumpCell{}
			if nv == nil {
				cell.None = true
			} else {
				cell.Value = fmt.Sprintf("₱%.2f", *nv)
				if pv != nil {
					d := *nv - *pv
					cell.Delta = fuelfmt(d)
					cell.Up = d > 0
				} else {
					cell.Delta = "—"
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		return row, any
	}
	if row, ok := mkrow("NCR", func(p string) (*float64, *float64, bool) {
		nv, nok := utilities.NCRCommon(*now, p)
		if !nok {
			return nil, nil, false
		}
		var pv *float64
		if prev != nil {
			if v, ok := utilities.NCRCommon(*prev, p); ok {
				vv := v
				pv = &vv
			}
		}
		return &nv, pv, true
	}); ok {
		rows = append(rows, row)
	}
	for _, c := range []string{"Parañaque", "Muntinlupa"} {
		if row, ok := mkrow(c, func(p string) (*float64, *float64, bool) {
			nv, present := utilities.CityCommon(now, c, p)
			if !present {
				return nil, nil, false
			}
			pv, _ := utilities.CityCommon(prev, c, p)
			return nv, pv, true
		}); ok {
			rows = append(rows, row)
		}
	}
	return nowRange, prevRange, rows
}

// AlertLink is one water-alert link row.
type AlertLink struct {
	Path   string
	Areas  string
	Window string
	Reason string
}

// SourceRow is one fetch-source row.
type SourceRow struct {
	Name    string
	URL     string
	Fetched string
	Status  int
}

// PostLink is one frozen-post link.
type PostLink struct {
	Path  string
	Label string
}

// PowerAlertLink is one Las Piñas power alert row (now+upcoming).
type PowerAlertLink struct {
	Path      string
	Areas     string
	Window    string
	Reason    string
	Trend     string
	SourceURL string
}

// PowerNearbyLink is one compact nearby power item (Parañaque,
// Muntinlupa, Bacoor) for the next 14 days.
type PowerNearbyLink struct {
	Title string
	Dates string
	URL   string
}

// WaterNearbyLink is one compact nearby water schedule row from
// Water.Nearby.
type WaterNearbyLink struct {
	City     string
	Areas    string
	Schedule string
}

// IndexView is the data for SITE/u/index.html.
type IndexView struct {
	Title       string
	Description string
	Canonical   string
	OGImage     string
	Root        string
	FuelTL      []Block
	FuelEN      []Block
	FuelJA      []Block
	Weeks       [][3]string // per-week TL/EN/JA lines (latest 6)
	PumpNow     string
	PumpPrev    string
	PumpRows    []PumpRow
	BillsTL     []Block
	BillsEN     []Block
	BillsJA     []Block
	Alerts      []AlertLink
	NoWater     string // "none, checked <time>" TL line when no alerts
	WaterNearby []WaterNearbyLink
	HasWater    bool // water section data present (keeps old pages byte-identical when false)
	PowerAlerts []PowerAlertLink
	NoPower     string // "none, checked <time>" TL line when power has no alerts
	NearbyPower []PowerNearbyLink
	HasPower    bool // power section present (nil Power omits the section, preserving old bytes)
	Sources     []SourceRow
	FuelLink    string
	BillsLink   string
	WaterLinks  []PostLink
	PowerLinks  []PostLink
}

// PostView is the data for one frozen post page.
type PostView struct {
	Title       string
	Description string
	Canonical   string
	OGImage     string
	OGAlt       string
	Published   string
	Root        string
	H1TL        string
	H1EN        string
	H1JA        string
	BlocksTL    []Block
	BlocksEN    []Block
	BlocksJA    []Block
	Footer      string
	Kind        string
	AsOf        string
}

// postPage builds a frozen post page.
func postPage(doc PostDoc, base string) (string, error) {
	tl, en, ja, footer := splitCaption(doc.Text)
	first := func(s string) string {
		for _, ln := range strings.Split(s, "\n") {
			if t := strings.TrimSpace(ln); t != "" {
				return t
			}
		}
		return ""
	}
	pub := doc.AsOf
	if (doc.Kind == "water" || doc.Kind == "power") && len(doc.Start) >= 10 {
		pub = doc.Start[:10]
	}
	if doc.Kind == "bills" && len(doc.AsOf) == 7 {
		pub = doc.AsOf + "-01"
	}
	v := PostView{
		Title:       first(tl) + " — Presyo Bantay",
		Description: first(tl),
		Canonical:   base + "u/" + doc.Kind + "/" + doc.AsOf + "/",
		OGImage:     base + "u/" + doc.Kind + "/" + doc.AsOf + "/" + doc.Kind + ".png",
		OGAlt:       first(tl),
		Published:   pub,
		Root:        "../../../",
		H1TL:        first(tl),
		H1EN:        first(en),
		H1JA:        first(ja),
		BlocksTL:    dropHead(toBlocks(tl)),
		BlocksEN:    dropHead(toBlocks(en)),
		BlocksJA:    dropHead(toBlocks(ja)),
		Footer:      footer,
		Kind:        doc.Kind,
		AsOf:        doc.AsOf,
	}
	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "upost", v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Rebuild regenerates SITE/u/index.html and SITE/data/utilities.json
// from the snapshot plus the frozen posts on disk.
func Rebuild(snap *utilities.Snapshot, sitePublic, base, gen string) error {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	docs, err := loadPosts(sitePublic)
	if err != nil {
		return err
	}
	latest := map[string]PostDoc{}
	for _, d := range docs {
		if cur, ok := latest[d.Kind]; !ok || d.AsOf > cur.AsOf {
			latest[d.Kind] = d
		}
	}
	v := IndexView{
		Title:       "Bayarin / Utilities / 光熱費 — Presyo Bantay",
		Description: "Bayarin sa bahay: kuryente, LPG, tubig — Presyo Bantay utilities watch",
		Canonical:   base + "u/",
		Root:        "../",
	}
	// Latest fuel caption + 6-week list + pump table.
	if len(snap.Fuel.Adjustments) > 0 {
		if text, err := utilities.RenderFuelPost(snap); err == nil {
			tl, en, ja, _ := splitCaption(text)
			v.FuelTL, v.FuelEN, v.FuelJA = toBlocks(tl), toBlocks(en), toBlocks(ja)
			n := len(snap.Fuel.Adjustments)
			if n > 6 {
				n = 6
			}
			for _, a := range snap.Fuel.Adjustments[:n] {
				t, e, j := fuelRangeLine(a)
				v.Weeks = append(v.Weeks, [3]string{t, e, j})
			}
		} else {
			logf("index: no fuel section: %s", err.Error())
		}
		now, _ := utilities.TrendTable(snap)
		if now != nil {
			v.PumpNow, v.PumpPrev, v.PumpRows = pumpTable(snap)
		}
	}
	// Latest bills caption.
	if len(snap.Meralco) > 0 && len(snap.LPG) > 0 {
		if text, err := utilities.RenderBillsPost(snap); err == nil {
			tl, en, ja, _ := splitCaption(text)
			v.BillsTL, v.BillsEN, v.BillsJA = toBlocks(tl), toBlocks(en), toBlocks(ja)
		} else {
			logf("index: no bills section: %s", err.Error())
		}
	}
	// Water alerts (own wording + links) or the none-checked line.
	alerts := utilities.BuildWaterAlerts(snap.Water)
	for _, a := range alerts {
		v.Alerts = append(v.Alerts, AlertLink{
			Path:   "water/" + a.ID + "/",
			Areas:  strings.Join(a.Areas, ", "),
			Window: a.WindowText,
			Reason: utilities.AlertReason(a.ReasonKey, 0),
		})
	}
	if len(v.Alerts) == 0 && snap.Water != nil {
		ctl, _, _ := utilities.ShortDay(snap.Water.CheckedAt)
		v.NoWater = "Walang naka-schedule na interruption — tiningnan " + ctl
	}
	if snap.Water != nil {
		v.HasWater = true
		for _, r := range snap.Water.Nearby {
			v.WaterNearby = append(v.WaterNearby, WaterNearbyLink{
				City:     r.City,
				Areas:    r.Barangays,
				Schedule: r.Schedule,
			})
		}
		if v.WaterNearby == nil {
			v.WaterNearby = []WaterNearbyLink{}
		}
	}
	// Power section: Las Piñas planned interruptions now+upcoming, each
	// with the official link and the trend; "none, checked <time>"
	// otherwise (a layout alarm shows only "no data right now" publicly).
	if snap.Power != nil {
		v.HasPower = true
		if snap.Power.LayoutAlarm != "" {
			ctl, _, _ := utilities.ShortDay(snap.Power.CheckedAt)
			if ctl == "" {
				ctl = gen[:10]
			}
			v.NoPower = "Walang datos sa ngayon — tiningnan " + ctl
		} else {
			nowManila, _ := time.Parse(time.RFC3339, gen)
			today := nowManila.In(time.FixedZone("Asia/Manila", 8*3600)).Format("2006-01-02")
			horizon21 := nowManila.In(time.FixedZone("Asia/Manila", 8*3600)).Add(21 * 24 * time.Hour).Format("2006-01-02")
			for _, a := range utilities.BuildPowerAlerts(snap.Power, nowManila) {
				if a.End.Format("2006-01-02") < today {
					continue
				}
				if a.Start.Format("2006-01-02") > horizon21 {
					continue
				}
				trend := ""
				if tl, _, _, ok := utilities.PowerTrendLines(&a); ok {
					trend = tl
				}
				areas := strings.Join(a.Barangays, ", ")
				// Window text for the site row: reuse the caption TL date.
				window := ""
				if text := utilities.RenderPowerAlertPost(a, false); text != "" {
					for _, ln := range strings.Split(text, "\n") {
						if strings.HasPrefix(ln, "📅 ") {
							window = strings.TrimPrefix(ln, "📅 ")
							break
						}
					}
				}
				v.PowerAlerts = append(v.PowerAlerts, PowerAlertLink{
					Path:      "power/" + a.ID + "/",
					Areas:     areas,
					Window:    window,
					Reason:    utilities.PowerReason(a.ReasonKey, 0),
					Trend:     trend,
					SourceURL: a.SourceURL,
				})
			}
			if len(v.PowerAlerts) == 0 {
				ctl, _, _ := utilities.ShortDay(snap.Power.CheckedAt)
				if ctl == "" {
					ctl = today
				}
				v.NoPower = "Walang naka-schedule na brownout — tiningnan " + ctl
			}
			// Nearby Parañaque/Muntinlupa/Bacoor items for the next 14 days.
			horizon14 := nowManila.In(time.FixedZone("Asia/Manila", 8*3600)).Add(14 * 24 * time.Hour).Format("2006-01-02")
			for _, it := range snap.Power.Items {
				if it.DateFrom == "" || it.DateTo == "" {
					continue
				}
				if it.DateTo < today || it.DateFrom > horizon14 {
					continue
				}
				isNearby := false
				for _, c := range it.Cities {
					if c == "Parañaque" || c == "Muntinlupa" || c == "Bacoor" {
						isNearby = true
						break
					}
				}
				if !isNearby {
					continue
				}
				isLP := false
				for _, c := range it.Cities {
					if c == "Las Piñas" {
						isLP = true
						break
					}
				}
				if isLP {
					continue
				}
				dates := it.DateFrom
				if it.DateTo != it.DateFrom {
					dates += "–" + it.DateTo
				}
				v.NearbyPower = append(v.NearbyPower, PowerNearbyLink{
					Title: it.Title,
					Dates: dates,
					URL:   it.URL,
				})
			}
			if v.NearbyPower == nil {
				v.NearbyPower = []PowerNearbyLink{}
			}
			if v.PowerAlerts == nil {
				v.PowerAlerts = []PowerAlertLink{}
			}
		}
	}
	for _, s := range snap.Sources {
		v.Sources = append(v.Sources, SourceRow{Name: s.Name, URL: s.URL, Fetched: s.FetchedAt, Status: s.HTTPStatus})
	}
	if v.Sources == nil {
		v.Sources = []SourceRow{}
	}
	// Frozen post links + manifest.
	manifest := UtilitiesDoc{GeneratedAt: gen}
	link := func(kind string) (PostLink, *UtilEntry, bool) {
		d, ok := latest[kind]
		if !ok {
			return PostLink{}, nil, false
		}
		id := d.AsOf
		entry := &UtilEntry{ID: id, Path: "u/" + kind + "/" + id + "/"}
		return PostLink{Path: kind + "/" + id + "/", Label: id}, entry, true
	}
	if l, e, ok := link("fuel"); ok {
		v.FuelLink = l.Path
		manifest.Fuel = e
	}
	if l, e, ok := link("bills"); ok {
		v.BillsLink = l.Path
		manifest.Bills = e
	}
	// Water links: every frozen alert page, newest first.
	var waters []PostDoc
	for _, d := range docs {
		if d.Kind == "water" {
			waters = append(waters, d)
		}
	}
	sort.Slice(waters, func(i, j int) bool { return waters[i].GeneratedAt > waters[j].GeneratedAt })
	for _, d := range waters {
		v.WaterLinks = append(v.WaterLinks, PostLink{Path: "water/" + d.AsOf + "/", Label: d.AsOf})
	}
	if len(waters) > 0 {
		manifest.Water = &UtilEntry{ID: waters[0].AsOf, Path: "u/water/" + waters[0].AsOf + "/"}
	}
	if v.WaterLinks == nil {
		v.WaterLinks = []PostLink{}
	}
	// Power links: every frozen power page, newest first.
	var powers []PostDoc
	for _, d := range docs {
		if d.Kind == "power" {
			powers = append(powers, d)
		}
	}
	sort.Slice(powers, func(i, j int) bool { return powers[i].GeneratedAt > powers[j].GeneratedAt })
	for _, d := range powers {
		v.PowerLinks = append(v.PowerLinks, PostLink{Path: "power/" + d.AsOf + "/", Label: d.AsOf})
	}
	if len(powers) > 0 {
		manifest.Power = &UtilEntry{ID: powers[0].AsOf, Path: "u/power/" + powers[0].AsOf + "/"}
	}
	if v.PowerLinks == nil {
		v.PowerLinks = []PostLink{}
	}
	if len(v.Weeks) > 0 {
		// OG image: latest fuel chart when present.
		if manifest.Fuel != nil {
			v.OGImage = base + manifest.Fuel.Path + "fuel.png"
		}
	}
	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "uindex", v); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(sitePublic, "u"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(sitePublic, "u", "index.html"), buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(sitePublic, "data", "utilities.json"), manifest); err != nil {
		return err
	}
	// Frozen post pages (Run only calls Rebuild after freezing, so
	// this refreshes HTML for every frozen post on disk).
	for _, d := range docs {
		html, err := postPage(d, base)
		if err != nil {
			return err
		}
		p := filepath.Join(sitePublic, "u", d.Kind, d.AsOf, "index.html")
		if err := os.WriteFile(p, []byte(html), 0o644); err != nil {
			return err
		}
	}
	return nil
}
