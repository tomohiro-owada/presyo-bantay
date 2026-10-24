// Package pdftext extracts positioned characters and painted vector paths
// from PDF pages, replicating pdfminer.six + pdfplumber semantics.
//
// Only low-level facilities of github.com/ledongthuc/pdf are used (file and
// xref parsing, stream decoding, the content-stream lexer, font Widths and
// text decoding). The content-stream interpreter below is handwritten so
// that the CTM is applied to painted paths, paths are classified into
// rect/line/curve exactly like pdfminer's paint_path, and per-glyph text
// advances match pdfminer's render_string_horizontal.
package pdftext

import (
	"fmt"
	"math"
	"os"

	"github.com/ledongthuc/pdf"
)

// Char is one decoded glyph with its bounding box, mirroring pdfminer's
// LTChar as exposed by pdfplumber (user-space X/Y plus top-based Top).
type Char struct {
	Text        string
	X0, X1      float64 // user space, y-up
	Y0, Y1      float64 // user space, y-up
	Top, Bottom float64 // top-based: Top = PageH - Y1
	Doctop      float64 // == Top (single page, initial_doctop 0)
	Upright     bool
	Size        float64 // == Y1 - Y0 for horizontal text
	FontName    string
	Adv         float64 // glyph advance in user-space units
	Matrix      [6]float64
}

// Rect is a painted axis-aligned rectangle, mirroring pdfminer's LTRect.
type Rect struct {
	X0, Y0, X1, Y1 float64
	Top, Bottom    float64
	Doctop         float64
	Width, Height  float64
	LineWidth      float64
}

// Line is a painted single straight segment, mirroring LTLine.
type Line struct {
	X0, Y0, X1, Y1 float64
	Top, Bottom    float64
	Doctop         float64
	Width, Height  float64 // bbox dimensions
	LineWidth      float64
}

// Curve is any other painted path, mirroring LTCurve. Points are stored in
// page coordinates (x, top-based y), like pdfplumber's curve["pts"], so
// axis-aligned segments can be turned into table edges.
type Curve struct {
	Pts [][2]float64
}

// Page holds one parsed PDF page (1-based numbering).
type Page struct {
	Width, Height float64
	Chars         []Char
	Rects         []Rect
	Lines         []Line
	Curves        []Curve
}

// Document is an open PDF file.
type Document struct {
	file   *os.File
	reader *pdf.Reader
}

// Open opens the PDF at path for reading.
func Open(path string) (*Document, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pdftext: open %s: %w", path, err)
	}
	return &Document{file: f, reader: r}, nil
}

// Close releases the underlying file.
func (d *Document) Close() error {
	return d.file.Close()
}

// NumPages returns the number of pages in the document.
func (d *Document) NumPages() int {
	return d.reader.NumPage()
}

// Page parses page num (1-based) into chars and vector paths.
func (d *Document) Page(num int) (Page, error) {
	p := d.reader.Page(num)
	if p.V.IsNull() {
		return Page{}, fmt.Errorf("pdftext: no page %d", num)
	}
	w, h := mediaBox(p.V)
	pg := Page{Width: w, Height: h}
	interp := &interpreter{pageH: h, fonts: map[string]*pdffont{}}
	contents := p.V.Key("Contents")
	if contents.IsNull() || (contents.Kind() == pdf.Array && contents.Len() == 0) {
		return pg, nil
	}
	if err := interp.run(p.V, contents); err != nil {
		return Page{}, err
	}
	pg.Chars = interp.chars
	pg.Rects = interp.rects
	pg.Lines = interp.lines
	pg.Curves = interp.curves
	return pg, nil
}

// mediaBox walks the page-parent chain for MediaBox (default 612x792).
func mediaBox(v pdf.Value) (float64, float64) {
	for cur := v; !cur.IsNull(); cur = cur.Key("Parent") {
		mb := cur.Key("MediaBox")
		if mb.Kind() == pdf.Array && mb.Len() >= 4 {
			x0 := mb.Index(0).Float64()
			y0 := mb.Index(1).Float64()
			x1 := mb.Index(2).Float64()
			y1 := mb.Index(3).Float64()
			return x1 - x0, y1 - y0
		}
	}
	return 612, 792
}

// resources walks the page-parent chain for Resources.
func resources(v pdf.Value) pdf.Value {
	for cur := v; !cur.IsNull(); cur = cur.Key("Parent") {
		r := cur.Key("Resources")
		if !r.IsNull() {
			return r
		}
	}
	return pdf.Value{}
}

// matrix is a PDF 6-element matrix (a, b, c, d, e, f).
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// mult replicates pdfminer utils.mult_matrix(m1, m0).
func mult(m1, m0 matrix) matrix {
	a1, b1, c1, d1, e1, f1 := m1[0], m1[1], m1[2], m1[3], m1[4], m1[5]
	a0, b0, c0, d0, e0, f0 := m0[0], m0[1], m0[2], m0[3], m0[4], m0[5]
	return matrix{
		a0*a1 + c0*b1,
		b0*a1 + d0*b1,
		a0*c1 + c0*d1,
		b0*c1 + d0*d1,
		a0*e1 + c0*f1 + e0,
		b0*e1 + d0*f1 + f0,
	}
}

// translate replicates pdfminer utils.translate_matrix(m, (x, y)).
func translate(m matrix, x, y float64) matrix {
	a, b, c, d, e, f := m[0], m[1], m[2], m[3], m[4], m[5]
	return matrix{a, b, c, d, x*a + y*c + e, x*b + y*d + f}
}

// applyPt replicates pdfminer utils.apply_matrix_pt.
func applyPt(m matrix, x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// applyRect replicates pdfminer utils.apply_matrix_rect (axis-aligned bbox
// of the transformed corners).
func applyRect(m matrix, x0, y0, x1, y1 float64) (float64, float64, float64, float64) {
	pts := [4][2]float64{{x0, y0}, {x0, y1}, {x1, y1}, {x1, y0}}
	ox0, oy0 := applyPt(m, pts[0][0], pts[0][1])
	lo_x, hi_x := ox0, ox0
	lo_y, hi_y := oy0, oy0
	for _, p := range pts[1:] {
		ox, oy := applyPt(m, p[0], p[1])
		lo_x = math.Min(lo_x, ox)
		hi_x = math.Max(hi_x, ox)
		lo_y = math.Min(lo_y, oy)
		hi_y = math.Max(hi_y, oy)
	}
	return lo_x, lo_y, hi_x, hi_y
}

// pdffont wraps a PDF font dictionary with decoded metrics.
type pdffont struct {
	pdfFont  *pdf.Font
	name     string
	descent  float64 // text-space units, forced <= 0 like pdfminer
	multi    bool    // multibyte (Type0) font
	fallback bool    // substituted empty font for missing Tf names
}

// fontName resolves the font's base name for Char.FontName.
func fontName(v pdf.Value) string {
	bf := v.Key("BaseFont").Name()
	if bf != "" {
		return bf
	}
	return "unknown"
}

func newFont(v pdf.Value) *pdffont {
	sub := v.Key("Subtype").Name()
	f := &pdffont{name: fontName(v), multi: sub == "Type0"}
	pf := pdf.Font{V: v}
	f.pdfFont = &pf
	fdesc := v.Key("FontDescriptor")
	d := 0.0
	if !fdesc.IsNull() {
		dd := fdesc.Key("Descent")
		if dd.Kind() == pdf.Integer || dd.Kind() == pdf.Real {
			d = dd.Float64() * 0.001
		}
	}
	if d > 0 {
		d = -d
	}
	f.descent = d
	return f
}

// width returns the glyph width in text-space units (pdfminer char_width:
// Widths[cid] * 0.001).
func (f *pdffont) width(cid int) float64 {
	if f.fallback || f.pdfFont == nil {
		return 0
	}
	return f.pdfFont.Width(cid) * 0.001
}

// decode maps one single-byte code to a rune (WinAnsi/Differences/ToUnicode
// handled by the underlying encoder).
func (f *pdffont) decode(b byte) rune {
	if f.fallback || f.pdfFont == nil {
		if b < 128 {
			return rune(b)
		}
		return rune(b)
	}
	s := f.pdfFont.Encoder().Decode(string([]byte{b}))
	runes := []rune(s)
	if len(runes) == 0 {
		return 0xFFFD
	}
	return runes[0]
}

// decodeStr decodes a whole raw string (used for multibyte fonts).
func (f *pdffont) decodeStr(raw string) []rune {
	if f.fallback || f.pdfFont == nil {
		out := make([]rune, 0, len(raw))
		for i := 0; i < len(raw); i++ {
			out = append(out, rune(raw[i]))
		}
		return out
	}
	return []rune(f.pdfFont.Encoder().Decode(raw))
}

// textState mirrors pdfminer PDFTextState.
type textState struct {
	font      *pdffont
	fontsize  float64
	charspace float64
	wordspace float64
	scaling   float64 // percent, default 100
	leading   float64 // note: TL stores -leading like pdfminer
	rise      float64
	tm        matrix
	lmX, lmY  float64 // linematrix point
}

// graphicState mirrors the parts of PDFGraphicState we need.
type graphicState struct {
	ctm       matrix
	linewidth float64
}

// pathSeg is one path-construction operation.
type pathSeg struct {
	op  byte // 'm', 'l', 'c', 'v', 'y', 'h'
	arg []float64
}

// interpreter holds the full content-stream interpretation state,
// mirroring pdfminer PDFPageInterpreter + PDFLayoutAnalyzer device.
type interpreter struct {
	pageH float64
	res   pdf.Value
	fonts map[string]*pdffont

	ts     textState
	gs     graphicState
	gstack []savedState

	curpath []pathSeg

	chars  []Char
	rects  []Rect
	lines  []Line
	curves []Curve
}

type savedState struct {
	ts textState
	gs graphicState
}

// num converts an Integer/Real operand to float64.
func num(v pdf.Value) (float64, bool) {
	switch v.Kind() {
	case pdf.Integer, pdf.Real:
		return v.Float64(), true
	}
	return 0, false
}

func (in *interpreter) getFont(name string) *pdffont {
	if f, ok := in.fonts[name]; ok {
		return f
	}
	fd := in.res.Key("Font")
	var f *pdffont
	if !fd.IsNull() {
		fv := fd.Key(name)
		if !fv.IsNull() {
			f = newFont(fv)
		}
	}
	if f == nil {
		// pdfminer non-strict: substitute an empty font for unknown names.
		f = &pdffont{name: name, fallback: true}
	}
	in.fonts[name] = f
	return f
}

// run interprets the page content stream(s).
func (in *interpreter) run(pageV, contents pdf.Value) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdftext: interpret: %v", r)
		}
	}()
	in.res = resources(pageV)
	in.ts = textState{scaling: 100, tm: identity}
	in.gs = graphicState{ctm: identity}
	pdf.Interpret(contents, func(stk *pdf.Stack, op string) {
		in.exec(stk, op)
	})
	return nil
}

// popArgs pops all current stack values, bottom-first like pdfminer operands.
func popArgs(stk *pdf.Stack) []pdf.Value {
	n := stk.Len()
	args := make([]pdf.Value, n)
	for i := n - 1; i >= 0; i-- {
		args[i] = stk.Pop()
	}
	return args
}

func (in *interpreter) exec(stk *pdf.Stack, op string) {
	switch op {
	// Graphics state
	case "q":
		popArgs(stk)
		in.gstack = append(in.gstack, savedState{ts: in.ts, gs: in.gs})
	case "Q":
		popArgs(stk)
		if len(in.gstack) > 0 {
			top := in.gstack[len(in.gstack)-1]
			in.gstack = in.gstack[:len(in.gstack)-1]
			in.ts, in.gs = top.ts, top.gs
		}
	case "cm":
		args := popArgs(stk)
		if len(args) != 6 {
			return
		}
		var m matrix
		for i := 0; i < 6; i++ {
			v, ok := num(args[i])
			if !ok {
				return
			}
			m[i] = v
		}
		in.gs.ctm = mult(m, in.gs.ctm)
	case "w":
		args := popArgs(stk)
		if len(args) != 1 {
			return
		}
		w, ok := num(args[0])
		if !ok {
			return
		}
		// pdfminer: linewidth scaled by CTM x-axis norm at set time.
		scale := math.Hypot(in.gs.ctm[0], in.gs.ctm[1])
		in.gs.linewidth = w * scale
	case "J", "j", "M", "d", "ri", "i", "gs":
		popArgs(stk) // tracked by pdfminer; irrelevant downstream
	case "G", "g", "RG", "rg", "K", "k", "CS", "cs", "SC", "sc", "SCN", "scn":
		popArgs(stk) // colors; irrelevant downstream

	// Path construction
	case "m":
		args := popArgs(stk)
		if len(args) != 2 {
			return
		}
		x, ok1 := num(args[0])
		y, ok2 := num(args[1])
		if !ok1 || !ok2 {
			return
		}
		in.curpath = append(in.curpath, pathSeg{'m', []float64{x, y}})
	case "l":
		args := popArgs(stk)
		if len(args) != 2 {
			return
		}
		x, ok1 := num(args[0])
		y, ok2 := num(args[1])
		if !ok1 || !ok2 {
			return
		}
		in.curpath = append(in.curpath, pathSeg{'l', []float64{x, y}})
	case "c":
		args := popArgs(stk)
		if len(args) != 6 {
			return
		}
		var a [6]float64
		for i := range args {
			v, ok := num(args[i])
			if !ok {
				return
			}
			a[i] = v
		}
		in.curpath = append(in.curpath, pathSeg{'c', a[:]})
	case "v":
		args := popArgs(stk)
		if len(args) != 4 {
			return
		}
		var a [4]float64
		for i := range args {
			v, ok := num(args[i])
			if !ok {
				return
			}
			a[i] = v
		}
		in.curpath = append(in.curpath, pathSeg{'v', a[:]})
	case "y":
		args := popArgs(stk)
		if len(args) != 4 {
			return
		}
		var a [4]float64
		for i := range args {
			v, ok := num(args[i])
			if !ok {
				return
			}
			a[i] = v
		}
		in.curpath = append(in.curpath, pathSeg{'y', a[:]})
	case "h":
		popArgs(stk)
		in.curpath = append(in.curpath, pathSeg{'h', nil})
	case "re":
		args := popArgs(stk)
		if len(args) != 4 {
			return
		}
		var a [4]float64
		for i := range args {
			v, ok := num(args[i])
			if !ok {
				return
			}
			a[i] = v
		}
		x, y, w, h := a[0], a[1], a[2], a[3]
		// pdfminer do_re: m, l, l, l, h.
		in.curpath = append(in.curpath,
			pathSeg{'m', []float64{x, y}},
			pathSeg{'l', []float64{x + w, y}},
			pathSeg{'l', []float64{x + w, y + h}},
			pathSeg{'l', []float64{x, y + h}},
			pathSeg{'h', nil},
		)

	// Path painting
	case "S", "s":
		args := popArgs(stk)
		_ = args
		if op == "s" {
			in.curpath = append(in.curpath, pathSeg{'h', nil})
		}
		in.paintPath()
	case "f", "F", "f*":
		popArgs(stk)
		if op == "F" {
			// pdfminer do_F (obsolete) is a no-op: path is NOT cleared.
			return
		}
		in.paintPath()
	case "B", "B*":
		popArgs(stk)
		in.paintPath()
	case "b", "b*":
		popArgs(stk)
		in.curpath = append(in.curpath, pathSeg{'h', nil})
		in.paintPath()
	case "n":
		popArgs(stk)
		in.curpath = nil
	case "W", "W*":
		popArgs(stk) // clipping paths produce no layout objects

	// Marked content / compat / XObjects: no layout objects.
	case "BMC", "BDC", "EMC", "MP", "DP", "BX", "EX", "Do", "BI", "ID", "EI":
		popArgs(stk)

	// Text object + state
	case "BT":
		popArgs(stk)
		in.ts.tm = identity
		in.ts.lmX, in.ts.lmY = 0, 0
	case "ET":
		popArgs(stk)
	case "Tc":
		args := popArgs(stk)
		if len(args) == 1 {
			if v, ok := num(args[0]); ok {
				in.ts.charspace = v
			}
		}
	case "Tw":
		args := popArgs(stk)
		if len(args) == 1 {
			if v, ok := num(args[0]); ok {
				in.ts.wordspace = v
			}
		}
	case "Tz":
		args := popArgs(stk)
		if len(args) == 1 {
			if v, ok := num(args[0]); ok {
				in.ts.scaling = v
			}
		}
	case "TL":
		args := popArgs(stk)
		if len(args) == 1 {
			if v, ok := num(args[0]); ok {
				in.ts.leading = -v
			}
		}
	case "Tf":
		args := popArgs(stk)
		if len(args) != 2 {
			return
		}
		name := args[0].Name()
		size, ok := num(args[1])
		if !ok {
			return
		}
		// pdfminer non-strict substitutes an empty font for unknown names
		// but still records the size.
		in.ts.font = in.getFont(name)
		in.ts.fontsize = size
	case "Tr", "Ts":
		args := popArgs(stk)
		if len(args) == 1 {
			if v, ok := num(args[0]); ok && op == "Ts" {
				in.ts.rise = v
			}
		}
	case "Td":
		args := popArgs(stk)
		if len(args) != 2 {
			return
		}
		tx, ok1 := num(args[0])
		ty, ok2 := num(args[1])
		if !ok1 || !ok2 {
			return
		}
		a, b, c, d, e, f := in.ts.tm[0], in.ts.tm[1], in.ts.tm[2], in.ts.tm[3], in.ts.tm[4], in.ts.tm[5]
		in.ts.tm = matrix{a, b, c, d, tx*a + ty*c + e, tx*b + ty*d + f}
		in.ts.lmX, in.ts.lmY = 0, 0
	case "TD":
		args := popArgs(stk)
		if len(args) != 2 {
			return
		}
		tx, ok1 := num(args[0])
		ty, ok2 := num(args[1])
		if !ok1 || !ok2 {
			return
		}
		a, b, c, d, e, f := in.ts.tm[0], in.ts.tm[1], in.ts.tm[2], in.ts.tm[3], in.ts.tm[4], in.ts.tm[5]
		in.ts.tm = matrix{a, b, c, d, tx*a + ty*c + e, tx*b + ty*d + f}
		in.ts.leading = ty
		in.ts.lmX, in.ts.lmY = 0, 0
	case "Tm":
		args := popArgs(stk)
		if len(args) != 6 {
			return
		}
		var m matrix
		for i := range args {
			v, ok := num(args[i])
			if !ok {
				return
			}
			m[i] = v
		}
		in.ts.tm = m
		in.ts.lmX, in.ts.lmY = 0, 0
	case "T*":
		popArgs(stk)
		a, b, c, d, e, f := in.ts.tm[0], in.ts.tm[1], in.ts.tm[2], in.ts.tm[3], in.ts.tm[4], in.ts.tm[5]
		in.ts.tm = matrix{a, b, c, d, in.ts.leading*c + e, in.ts.leading*d + f}
		in.ts.lmX, in.ts.lmY = 0, 0
	case "Tj":
		args := popArgs(stk)
		if len(args) != 1 || args[0].Kind() != pdf.String {
			return
		}
		in.renderString([]pdf.Value{args[0]})
	case "TJ":
		args := popArgs(stk)
		if len(args) != 1 || args[0].Kind() != pdf.Array {
			return
		}
		arr := args[0]
		seq := make([]pdf.Value, 0, arr.Len())
		for i := 0; i < arr.Len(); i++ {
			seq = append(seq, arr.Index(i))
		}
		in.renderString(seq)
	case "'":
		args := popArgs(stk)
		if len(args) != 1 {
			return
		}
		in.doTStar()
		if args[0].Kind() == pdf.String {
			in.renderString([]pdf.Value{args[0]})
		}
	case "\"":
		args := popArgs(stk)
		if len(args) != 3 {
			return
		}
		// NB: pdfminer do__w sets spacing but (unlike the spec's operator)
		// does NOT move to the next line. Replicated here.
		if v, ok := num(args[0]); ok {
			in.ts.wordspace = v
		}
		if v, ok := num(args[1]); ok {
			in.ts.charspace = v
		}
		if args[2].Kind() == pdf.String {
			in.renderString([]pdf.Value{args[2]})
		}
	default:
		popArgs(stk) // unknown op: drop operands, ignore
	}
}

func (in *interpreter) doTStar() {
	a, b, c, d, e, f := in.ts.tm[0], in.ts.tm[1], in.ts.tm[2], in.ts.tm[3], in.ts.tm[4], in.ts.tm[5]
	in.ts.tm = matrix{a, b, c, d, in.ts.leading*c + e, in.ts.leading*d + f}
	in.ts.lmX, in.ts.lmY = 0, 0
}

// paintPath classifies the current path exactly like pdfminer's
// converter.paint_path and emits LTRect/LTLine/LTCurve equivalents.
func (in *interpreter) paintPath() {
	path := in.curpath
	in.curpath = nil
	if len(path) == 0 {
		return
	}
	shape := make([]byte, 0, len(path))
	for _, s := range path {
		shape = append(shape, s.op)
	}
	if shape[0] != 'm' {
		return
	}
	// Multiple subpaths: recurse per subpath (regex m[^m]+).
	if countByte(shape, 'm') > 1 {
		start := 0
		for i := 1; i <= len(shape); i++ {
			if i == len(shape) || shape[i] == 'm' {
				in.paintOne(path[start:i])
				start = i
			}
		}
		return
	}
	in.paintOne(path)
}

func countByte(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

// paintOne classifies a single-subpath paint.
func (in *interpreter) paintOne(path []pathSeg) {
	shape := make([]byte, 0, len(path))
	for _, s := range path {
		shape = append(shape, s.op)
	}
	rawPts := make([][2]float64, 0, len(path))
	for _, s := range path {
		if s.op == 'h' {
			rawPts = append(rawPts, [2]float64{path[0].arg[0], path[0].arg[1]})
		} else {
			a := s.arg
			rawPts = append(rawPts, [2]float64{a[len(a)-2], a[len(a)-1]})
		}
	}
	pts := make([][2]float64, len(rawPts))
	for i, p := range rawPts {
		x, y := applyPt(in.gs.ctm, p[0], p[1])
		pts[i] = [2]float64{x, y}
	}
	s := string(shape)
	// Drop a redundant "l" on a path closed with "h".
	if len(s) > 3 && s[len(s)-2:] == "lh" && pts[len(pts)-2] == pts[0] {
		s = s[:len(s)-2] + "h"
		pts = pts[:len(pts)-1]
	}
	switch s {
	case "mlh", "ml":
		in.lines = append(in.lines, Line{
			X0: pts[0][0], Y0: pts[0][1], X1: pts[1][0], Y1: pts[1][1],
			Top:       in.pageH - math.Max(pts[0][1], pts[1][1]),
			Bottom:    in.pageH - math.Min(pts[0][1], pts[1][1]),
			Doctop:    in.pageH - math.Max(pts[0][1], pts[1][1]),
			Width:     math.Abs(pts[1][0] - pts[0][0]),
			Height:    math.Abs(pts[1][1] - pts[0][1]),
			LineWidth: in.gs.linewidth,
		})
	case "mlllh", "mllll":
		x0, y0 := pts[0][0], pts[0][1]
		x1, y1 := pts[1][0], pts[1][1]
		x2, y2 := pts[2][0], pts[2][1]
		x3, y3 := pts[3][0], pts[3][1]
		closed := pts[0] == pts[4]
		square := (x0 == x1 && y1 == y2 && x2 == x3 && y3 == y0) ||
			(y0 == y1 && x1 == x2 && y2 == y3 && x3 == x0)
		if closed && square {
			// LTRect bbox = (pts[0], pts[2]), no normalization.
			rx0, ry0, rx1, ry1 := x0, y0, x2, y2
			in.rects = append(in.rects, Rect{
				X0: rx0, Y0: ry0, X1: rx1, Y1: ry1,
				Top:       in.pageH - ry1,
				Bottom:    in.pageH - ry0,
				Doctop:    in.pageH - ry1,
				Width:     rx1 - rx0,
				Height:    ry1 - ry0,
				LineWidth: in.gs.linewidth,
			})
			return
		}
		in.addCurve(pts)
	default:
		in.addCurve(pts)
	}
}

// addCurve stores a curve in page coordinates like pdfplumber curve["pts"].
func (in *interpreter) addCurve(pts [][2]float64) {
	cp := make([][2]float64, len(pts))
	for i, p := range pts {
		cp[i] = [2]float64{p[0], in.pageH - p[1]}
	}
	in.curves = append(in.curves, Curve{Pts: cp})
}

// renderString replicates pdfminer PDFTextDevice.render_string_horizontal
// (all DA table-page fonts are horizontal).
func (in *interpreter) renderString(seq []pdf.Value) {
	ts := &in.ts
	if ts.font == nil {
		return // pdfminer non-strict: no font, no render
	}
	font := ts.font
	fontsize := ts.fontsize
	scaling := ts.scaling * 0.01
	charspace := ts.charspace * scaling
	wordspace := ts.wordspace * scaling
	if font.multi {
		wordspace = 0
	}
	rise := ts.rise
	dxscale := 0.001 * fontsize * scaling
	mat := mult(ts.tm, in.gs.ctm)
	x, y := ts.lmX, ts.lmY
	needcharspace := false
	for _, obj := range seq {
		switch obj.Kind() {
		case pdf.Integer, pdf.Real:
			x -= obj.Float64() * dxscale
			needcharspace = true
		case pdf.String:
			raw := obj.RawString()
			if font.multi {
				runes := font.decodeStr(raw)
				for _, r := range runes {
					if needcharspace {
						x += charspace
					}
					// Multibyte widths are unavailable without the
					// descendant CID font metrics; emit zero-advance
					// chars (never hit on DA table pages 1-3).
					in.renderChar(mat, x, y, font, fontsize, scaling, rise, r, 0)
					needcharspace = true
				}
			} else {
				for i := 0; i < len(raw); i++ {
					cid := int(raw[i])
					if needcharspace {
						x += charspace
					}
					r := font.decode(raw[i])
					textwidth := font.width(cid)
					adv := in.renderChar(mat, x, y, font, fontsize, scaling, rise, r, textwidth)
					x += adv
					if cid == 32 && wordspace != 0 {
						x += wordspace
					}
					needcharspace = true
				}
			}
		}
	}
	ts.lmX, ts.lmY = x, y
}

// renderChar replicates pdfminer converter.render_char + LTChar geometry.
// Returns the advance (textwidth * fontsize * scaling).
func (in *interpreter) renderChar(mat matrix, x, y float64, font *pdffont, fontsize, scaling, rise float64, r rune, textwidth float64) float64 {
	adv := textwidth * fontsize * scaling
	cm := translate(mat, x, y)
	a, b, c, d := cm[0], cm[1], cm[2], cm[3]
	upright := a*d*scaling > 0 && b*c <= 0
	descent := font.descent
	bx0, by0, bx1, by1 := 0.0, descent*fontsize+rise, adv, descent*fontsize+rise+fontsize
	x0, y0, x1, y1 := applyRect(cm, bx0, by0, bx1, by1)
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	h := in.pageH
	in.chars = append(in.chars, Char{
		Text: string(r),
		X0:   x0, X1: x1, Y0: y0, Y1: y1,
		Top:      h - y1,
		Bottom:   h - y0,
		Doctop:   h - y1,
		Upright:  upright,
		Size:     y1 - y0,
		FontName: font.name,
		Adv:      adv,
		Matrix:   [6]float64{cm[0], cm[1], cm[2], cm[3], cm[4], cm[5]},
	})
	return adv
}
