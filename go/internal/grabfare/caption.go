package grabfare

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const Separator = "━━━━━━━━━━"

func shortenCaption(s string) string {
	s = strings.ReplaceAll(s, "Festival Supermall Alabang", "Festival Mall")
	s = strings.ReplaceAll(s, "Alabang-Zapote Road", "Alabang-Zapote Rd")
	return s
}

func captionRoute(r RouteOut) string {
	from := shortenCaption(r.From)
	to := shortenCaption(r.To)
	via := shortenCaption(r.Via)
	if strings.TrimSpace(via) != "" {
		return from + " → " + to + " (" + via + ")"
	}
	return from + " → " + to
}

// CardSubtitle is the route line under the FB card title:
// "<pickup> → Festival Mall · Alabang-Zapote Rd" (shortened like the caption).
// Empty when the route is empty.
func CardSubtitle(r RouteOut) string {
	from := strings.TrimSpace(shortenCaption(r.From))
	to := strings.TrimSpace(shortenCaption(r.To))
	via := strings.TrimSpace(shortenCaption(r.Via))
	if from == "" && to == "" {
		return ""
	}
	s := from + " \u2192 " + to
	if via != "" {
		s += " \u00b7 " + via
	}
	return s
}

func RenderCaption(o *Output) string {
	if o == nil {
		return ""
	}
	hhmm := ""
	if o.AsOfHHMM != nil {
		hhmm = *o.AsOfHHMM
	}
	route := captionRoute(o.Route)
	site := "https://abalol.xyz/presyo-bantay/"

	tlBlock := ""
	{
		var lines []string
		lines = append(lines, "🚕 "+o.Text.Tl.Title+": "+route)
		money := "💰 " + o.Text.Tl.Headline
		if o.Fare != nil && o.Fare.Standard != nil {
			money += " · Grab Standard " + FormatPeso(*o.Fare.Standard)
		}
		money += " · " + o.Text.Tl.Asof
		lines = append(lines, money)
		trend := TrendTextTl(o.Trend)
		if strings.TrimSpace(trend) != "" {
			lines = append(lines, "📈 "+trend)
		}
		if o.VsYesterday != nil {
			vy := YesterdayTextTl(*o.VsYesterday)
			if strings.TrimSpace(vy) != "" {
				lines = append(lines, "📈 "+vy)
			}
		}
		if o.Traffic != nil && o.Text.Tl.Traffic != "" {
			lines = append(lines, "🚦 "+o.Text.Tl.Traffic)
		}
		if o != nil && o.Bike != nil {
			lines = append(lines, "🏍️ Motor taxi (Move It): "+FormatRange(*o.Bike.Min, *o.Bike.Max)+" · "+bikeAsofShort(o.Bike, "tl"))
		}
		if o.Focus {
			lines = append(lines, "🗺️ Malapitan: Colours Town Center · SM Southmall · Village Square sa Alabang-Zapote Rd")
		}
		if strings.TrimSpace(o.Text.Tl.PickupNote) != "" {
			lines = append(lines, "📍 "+o.Text.Tl.PickupNote)
		}
		lines = append(lines, o.Text.Tl.Note)
		tlBlock = strings.Join(lines, "\n")
	}

	enBlock := ""
	{
		var lines []string
		lines = append(lines, "🇬🇧 "+o.Text.En.Title+": "+route)
		money := "💰 " + o.Text.En.Headline
		if o.Fare != nil && o.Fare.Standard != nil {
			money += " · Grab Standard " + FormatPeso(*o.Fare.Standard)
		}
		money += " · " + o.Text.En.Asof
		lines = append(lines, money)
		trend := TrendTextEn(o.Trend)
		if strings.TrimSpace(trend) != "" {
			lines = append(lines, "📈 "+trend)
		}
		if o.VsYesterday != nil {
			vy := YesterdayTextEn(*o.VsYesterday)
			if strings.TrimSpace(vy) != "" {
				lines = append(lines, "📈 "+vy)
			}
		}
		if o.Traffic != nil && o.Text.En.Traffic != "" {
			lines = append(lines, "🚦 "+o.Text.En.Traffic)
		}
		if o != nil && o.Bike != nil {
			lines = append(lines, "🏍️ Motor taxi (Move It): "+FormatRange(*o.Bike.Min, *o.Bike.Max)+" · "+bikeAsofShort(o.Bike, "en"))
		}
		if o.Focus {
			lines = append(lines, "🗺️ Close-up: Colours Town Center · SM Southmall · Village Square on Alabang-Zapote Rd")
		}
		if strings.TrimSpace(o.Text.En.PickupNote) != "" {
			lines = append(lines, "📍 "+o.Text.En.PickupNote)
		}
		lines = append(lines, o.Text.En.Note)
		enBlock = strings.Join(lines, "\n")
	}

	jaBlock := ""
	{
		var lines []string
		lines = append(lines, "🇯🇵 "+o.Text.Ja.Title+": "+route)
		money := "💰 " + o.Text.Ja.Headline
		if o.Fare != nil && o.Fare.Standard != nil {
			money += " · グラブ スタンダード " + FormatPeso(*o.Fare.Standard)
		}
		money += " · " + o.Text.Ja.Asof
		lines = append(lines, money)
		trend := TrendTextJa(o.Trend)
		if strings.TrimSpace(trend) != "" {
			lines = append(lines, "📈 "+trend)
		}
		if o.VsYesterday != nil {
			vy := YesterdayTextJa(*o.VsYesterday)
			if strings.TrimSpace(vy) != "" {
				lines = append(lines, "📈 "+vy)
			}
		}
		if o.Traffic != nil && o.Text.Ja.Traffic != "" {
			lines = append(lines, "🚦 "+o.Text.Ja.Traffic)
		}
		if o != nil && o.Bike != nil {
			lines = append(lines, "🏍️ Motor taxi (Move It): "+FormatRange(*o.Bike.Min, *o.Bike.Max)+" · "+bikeAsofShort(o.Bike, "ja"))
		}
		if o.Focus {
			lines = append(lines, "🗺️ 拡大: Alabang-Zapote Rd の Colours Town Center・SM Southmall・Village Square 付近")
		}
		if strings.TrimSpace(o.Text.Ja.PickupNote) != "" {
			lines = append(lines, "📍 "+o.Text.Ja.PickupNote)
		}
		lines = append(lines, o.Text.Ja.Note)
		jaBlock = strings.Join(lines, "\n")
	}

	sourceLine := "Source: Grab app quote at that time · Not an official fare · Map: Grab app screenshot · Hindi opisyal na pamasahe"
	if o != nil && o.Bike != nil {
		sourceLine += " · Bike fare: Move It app quote"
	}
	footer := "Details: " + site + "\n" + sourceLine

	_ = hhmm
	return tlBlock + "\n" + Separator + "\n" + enBlock + "\n" + Separator + "\n" + jaBlock + "\n" + Separator + "\n" + footer
}

func CheckCaption(text string, o *Output) []string {
	var problems []string
	add := func(s string) { problems = append(problems, s) }

	parts := strings.Split(text, Separator)
	if len(parts) != 4 {
		add(fmt.Sprintf("want 4 blocks separated by %q, got %d", Separator, len(parts)))
	}
	for i, p := range parts {
		if strings.TrimSpace(p) == "" {
			add(fmt.Sprintf("block %d empty", i+1))
		}
	}
	if strings.Contains(text, "$") {
		add("dollar sign")
	}
	if strings.Contains(text, "NaN") {
		add("NaN")
	}
	// nested parentheses
	depth := 0
	nested := false
	for _, r := range text {
		if r == '(' {
			depth++
			if depth > 1 {
				nested = true
			}
		} else if r == ')' {
			if depth > 0 {
				depth--
			}
		}
	}
	if nested {
		add("nested parentheses")
	}
	// allowed values
	allowedPeso := map[int64]bool{}
	addPeso := func(f float64) { allowedPeso[int64(math.Round(f))] = true }
	if o != nil && o.Fare != nil {
		addPeso(o.Fare.Min)
		addPeso(o.Fare.Max)
		if o.Fare.Standard != nil {
			addPeso(*o.Fare.Standard)
		}
	}
	if o != nil && o.Bike != nil {
		if o.Bike.Min != nil {
			addPeso(*o.Bike.Min)
		}
		if o.Bike.Max != nil {
			addPeso(*o.Bike.Max)
		}
	}
	if o != nil {
		for _, tr := range []*TrendOut{&o.Trend, o.VsYesterday} {
			if tr == nil {
				continue
			}
			if tr.DeltaPesos != nil {
				addPeso(math.Abs(*tr.DeltaPesos))
			}
		}
	}
	allowedPct := map[int64]bool{}
	addPct := func(f float64) { allowedPct[int64(math.Round(math.Abs(f)))] = true }
	if o != nil && o.Traffic != nil && o.Traffic.RedPct != nil {
		addPct(*o.Traffic.RedPct)
	}
	if o != nil {
		for _, tr := range []*TrendOut{&o.Trend, o.VsYesterday} {
			if tr == nil || tr.DeltaPct == nil {
				continue
			}
			addPct(*tr.DeltaPct)
		}
	}
	allowedHHMM := map[string]bool{}
	if o != nil && o.AsOfHHMM != nil {
		allowedHHMM[*o.AsOfHHMM] = true
	}
	if o != nil {
		for _, tr := range []*TrendOut{&o.Trend, o.VsYesterday} {
			if tr != nil && tr.PreviousHHMM != nil {
				allowedHHMM[*tr.PreviousHHMM] = true
			}
		}
	}
	if o != nil && o.Pickup != nil && o.Pickup.SinceHHMM != "" {
		allowedHHMM[o.Pickup.SinceHHMM] = true
	}
	if o != nil && o.Bike != nil && o.Bike.AsOfHHMM != nil {
		allowedHHMM[*o.Bike.AsOfHHMM] = true
	}

	pesoRe := regexp.MustCompile("\u20b1([0-9,]+)")
	rangeRe := regexp.MustCompile("\u2013([0-9,]+)")
	pctRe := regexp.MustCompile(`([0-9]+)%`)
	hhmmRe := regexp.MustCompile(`([0-9]{2}:[0-9]{2})`)

	for _, m := range pesoRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
		if !allowedPeso[n] {
			add("peso " + m[0] + " not in output")
		}
	}
	for _, m := range rangeRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
		if !allowedPeso[n] {
			add("range " + m[0] + " not in output")
		}
	}
	for _, m := range pctRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		if !allowedPct[n] {
			add("pct " + m[1] + "% not in output")
		}
	}
	for _, m := range hhmmRe.FindAllStringSubmatch(text, -1) {
		if !allowedHHMM[m[1]] {
			add("hhmm " + m[1] + " not in output")
		}
	}

	// trend line + disclaimer per language block
	noneTexts := []string{"Wala pang naunang reading", "No earlier reading yet", "以前の記録はまだありません"}
	if len(parts) >= 3 {
		blocks := []struct {
			idx       int
			trendNeed bool
			disc      string
		}{
			{0, true, "Hindi opisyal"},
			{1, true, "Not an official fare"},
			{2, true, "公式運賃ではありません"},
		}
		for _, b := range blocks {
			block := parts[b.idx]
			hasTrend := strings.Contains(block, "▲") || strings.Contains(block, "▼") || strings.Contains(block, "＝")
			for _, nt := range noneTexts {
				if strings.Contains(block, nt) {
					hasTrend = true
				}
			}
			if !hasTrend {
				add(fmt.Sprintf("block %d missing trend line", b.idx+1))
			}
			if !strings.Contains(block, b.disc) {
				add(fmt.Sprintf("block %d missing disclaimer", b.idx+1))
			}
		}
	}

	// traffic line presence
	if o != nil {
		trafficPresent := strings.Contains(text, "🚦")
		if (o.Traffic != nil) != trafficPresent {
			add("traffic line presence mismatch")
		}
	}

	// URLs
	site := "https://abalol.xyz/presyo-bantay/"
	// remove site url occurrences
	cleaned := strings.ReplaceAll(text, site, "")
	if strings.Contains(cleaned, "http") {
		add("unexpected http")
	}
	for _, bad := range []string{"ssh", "/home", "@"} {
		if strings.Contains(text, bad) {
			add("forbidden token: " + bad)
		}
	}
	// IPs
	ipRe := regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`)
	if ipRe.MatchString(text) {
		add("ip address")
	}
	// only URL site
	urlRe := regexp.MustCompile(`https?://[^\s]+`)
	for _, m := range urlRe.FindAllString(text, -1) {
		if m != site && strings.TrimRight(m, ".,") != site {
			add("unexpected url: " + m)
		}
	}

	// pickup name presence
	if o != nil && o.Pickup != nil {
		if !strings.Contains(text, o.Pickup.Name) {
			add("pickup name missing")
		}
	}
	// forbidden words
	badWords := []string{"gsm", "moveit", "green gsm"}
	if !(o != nil && o.Bike != nil) {
		badWords = append(badWords, "move it")
	}
	lower := strings.ToLower(text)
	for _, bad := range badWords {
		if strings.Contains(lower, bad) {
			add("forbidden word: " + bad)
		}
	}
	if o != nil && o.Bike != nil {
		if !strings.Contains(text, "Move It") {
			add("missing Move It")
		}
	}

	return problems
}

// bikeAsofShort is the caption's short as-of phrase for the bike line. A quote
// older than the freshness window is labelled as the last quote.
func bikeAsofShort(b *BikeOut, lang string) string {
	hhmm := ""
	if b.AsOfHHMM != nil {
		hhmm = *b.AsOfHHMM
	}
	switch lang {
	case "tl":
		if b.Fresh {
			return "Ayon sa Move It app noong " + hhmm
		}
		return "Huling Move It quote: " + hhmm
	case "ja":
		if b.Fresh {
			return hhmm + "時点 · Move Itアプリ表示"
		}
		return "最終Move It表示: " + hhmm
	}
	if b.Fresh {
		return "As of " + hhmm + " · Move It app quote"
	}
	return "Last Move It quote: " + hhmm
}
