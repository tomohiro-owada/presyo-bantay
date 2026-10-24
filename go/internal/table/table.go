// Package table ports pdfplumber's default "lines" extract_tables algorithm:
// rect/line/curve edges -> snap (3) -> join (3) -> intersections (3) ->
// cells -> tables. Cell text = chars whose center falls inside the cell,
// grouped into lines (y tolerance 3) with words split on x gaps > 3,
// lines joined by "\n".
package table

import (
	"math"
	"sort"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/pdftext"
)

const (
	SnapTolerance          = 3.0
	JoinTolerance          = 3.0
	IntersectionTolerance  = 3.0
	EdgeMinLength          = 3.0
	EdgeMinLengthPrefilter = 1.0
)

// Edge is one axis-aligned table edge.
type Edge struct {
	X0, X1, Top, Bottom float64
	Width, Height       float64
	Orient              byte // 'h' or 'v'
}

// BBox is (x0, top, x1, bottom).
type BBox [4]float64

// RectToEdges mirrors pdfplumber utils.rect_to_edges: [top, bottom, left,
// right]. Only the fields used downstream are kept.
func RectToEdges(r pdftext.Rect) []Edge {
	w := r.X1 - r.X0
	h := r.Bottom - r.Top
	top := Edge{X0: r.X0, X1: r.X1, Top: r.Top, Bottom: r.Top, Width: w, Height: 0, Orient: 'h'}
	bottom := Edge{X0: r.X0, X1: r.X1, Top: r.Top + h, Bottom: r.Bottom, Width: w, Height: 0, Orient: 'h'}
	left := Edge{X0: r.X0, X1: r.X0, Top: r.Top, Bottom: r.Bottom, Width: 0, Height: h, Orient: 'v'}
	right := Edge{X0: r.X1, X1: r.X1, Top: r.Top, Bottom: r.Bottom, Width: 0, Height: h, Orient: 'v'}
	return []Edge{top, bottom, left, right}
}

// LineToEdge mirrors pdfplumber utils.line_to_edge.
func LineToEdge(l pdftext.Line) Edge {
	o := byte('v')
	if l.Top == l.Bottom {
		o = 'h'
	}
	return Edge{X0: l.X0, X1: l.X1, Top: l.Top, Bottom: l.Bottom, Width: l.Width, Height: l.Height, Orient: o}
}

// CurveToEdges mirrors pdfplumber utils.curve_to_edges, dropping diagonal
// segments (orientation None), which filter_edges would exclude anyway.
func CurveToEdges(c pdftext.Curve) []Edge {
	var out []Edge
	for i := 0; i+1 < len(c.Pts); i++ {
		p0, p1 := c.Pts[i], c.Pts[i+1]
		var o byte
		switch {
		case p0[0] == p1[0]:
			o = 'v'
		case p0[1] == p1[1]:
			o = 'h'
		default:
			continue
		}
		out = append(out, Edge{
			X0: math.Min(p0[0], p1[0]), X1: math.Max(p0[0], p1[0]),
			Top: math.Min(p0[1], p1[1]), Bottom: math.Max(p0[1], p1[1]),
			Width: math.Abs(p0[0] - p1[0]), Height: math.Abs(p0[1] - p1[1]),
			Orient: o,
		})
	}
	return out
}

// PageEdges mirrors pdfplumber Container.edges: line edges + rect edges +
// curve edges, in that order.
func PageEdges(p pdftext.Page) []Edge {
	var edges []Edge
	for _, l := range p.Lines {
		edges = append(edges, LineToEdge(l))
	}
	for _, r := range p.Rects {
		edges = append(edges, RectToEdges(r)...)
	}
	for _, c := range p.Curves {
		edges = append(edges, CurveToEdges(c)...)
	}
	return edges
}

// FilterEdges mirrors pdfplumber utils.filter_edges.
func FilterEdges(edges []Edge, orient byte, minLength float64) []Edge {
	var out []Edge
	for _, e := range edges {
		if orient != 0 && e.Orient != orient {
			continue
		}
		dim := e.Width
		if e.Orient == 'v' {
			dim = e.Height
		}
		if dim >= minLength {
			out = append(out, e)
		}
	}
	return out
}

// clusterByKey mirrors pdfplumber utils.cluster_objects without
// preserve_order: group items by key within tolerance, clusters ordered by
// ascending key, items within a cluster in original relative order.
func clusterByKey[T any](items []T, key func(T) float64, tol float64) [][]T {
	if len(items) == 0 {
		return nil
	}
	uniq := map[float64]struct{}{}
	for _, it := range items {
		uniq[key(it)] = struct{}{}
	}
	vals := make([]float64, 0, len(uniq))
	for v := range uniq {
		vals = append(vals, v)
	}
	sort.Float64s(vals)
	// cluster_list over sorted unique values.
	groups := [][]float64{{vals[0]}}
	last := vals[0]
	for _, x := range vals[1:] {
		if x <= last+tol {
			groups[len(groups)-1] = append(groups[len(groups)-1], x)
		} else {
			groups = append(groups, []float64{x})
		}
		last = x
	}
	idOf := map[float64]int{}
	for i, g := range groups {
		for _, v := range g {
			idOf[v] = i
		}
	}
	// Stable sort by cluster id.
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return idOf[key(items[idx[a]])] < idOf[key(items[idx[b]])]
	})
	var out [][]T
	for _, i := range idx {
		id := idOf[key(items[i])]
		if len(out) == 0 || idOf[key(out[len(out)-1][0])] != id {
			out = append(out, []T{items[i]})
		} else {
			out[len(out)-1] = append(out[len(out)-1], items[i])
		}
	}
	return out
}

// snapEdges mirrors pdfplumber table.snap_edges.
func snapEdges(edges []Edge, xTol, yTol float64) []Edge {
	var vs, hs []Edge
	for _, e := range edges {
		if e.Orient == 'v' {
			vs = append(vs, e)
		} else {
			hs = append(hs, e)
		}
	}
	snap := func(objs []Edge, attr byte, tol float64) []Edge {
		key := func(e Edge) float64 {
			if attr == 'x' {
				return e.X0
			}
			return e.Top
		}
		clusters := clusterByKey(objs, key, tol)
		var out []Edge
		for _, c := range clusters {
			sum := 0.0
			for _, e := range c {
				sum += key(e)
			}
			avg := sum / float64(len(c))
			for _, e := range c {
				d := avg - key(e)
				if attr == 'x' {
					e.X0 += d
					e.X1 += d
				} else {
					e.Top += d
					e.Bottom += d
				}
				out = append(out, e)
			}
		}
		return out
	}
	return append(snap(vs, 'x', xTol), snap(hs, 't', yTol)...)
}

// joinEdgeGroup mirrors pdfplumber table.join_edge_group.
func joinEdgeGroup(edges []Edge, orient byte, tol float64) []Edge {
	if len(edges) == 0 {
		return nil
	}
	sorted := append([]Edge(nil), edges...)
	if orient == 'h' {
		sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].X0 < sorted[b].X0 })
	} else {
		sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Top < sorted[b].Top })
	}
	joined := []Edge{sorted[0]}
	for _, e := range sorted[1:] {
		last := joined[len(joined)-1]
		var minP, maxP, lastMax float64
		if orient == 'h' {
			minP, maxP, lastMax = e.X0, e.X1, last.X1
		} else {
			minP, maxP, lastMax = e.Top, e.Bottom, last.Bottom
		}
		if minP <= lastMax+tol {
			if maxP > lastMax {
				if orient == 'h' {
					last.X1 = maxP
					last.Width = maxP - last.X0
				} else {
					last.Bottom = maxP
					last.Height = maxP - last.Top
				}
				joined[len(joined)-1] = last
			}
		} else {
			joined = append(joined, e)
		}
	}
	return joined
}

// mergeEdges mirrors pdfplumber table.merge_edges with default settings.
func mergeEdges(edges []Edge, snapX, snapY, joinX, joinY float64) []Edge {
	if snapX > 0 || snapY > 0 {
		edges = snapEdges(edges, snapX, snapY)
	}
	type group struct {
		o byte
		p float64
	}
	// Stable sort by (orientation, pos): 'h' < 'v' like Python tuples.
	sorted := append([]Edge(nil), edges...)
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a].Orient != sorted[b].Orient {
			return sorted[a].Orient < sorted[b].Orient
		}
		if sorted[a].Orient == 'h' {
			return sorted[a].Top < sorted[b].Top
		}
		return sorted[a].X0 < sorted[b].X0
	})
	var out []Edge
	for i := 0; i < len(sorted); {
		j := i + 1
		for j < len(sorted) && sorted[j].Orient == sorted[i].Orient &&
			((sorted[i].Orient == 'h' && sorted[j].Top == sorted[i].Top) ||
				(sorted[i].Orient == 'v' && sorted[j].X0 == sorted[i].X0)) {
			j++
		}
		tol := joinY
		if sorted[i].Orient == 'h' {
			tol = joinX
		}
		out = append(out, joinEdgeGroup(sorted[i:j], sorted[i].Orient, tol)...)
		i = j
	}
	return out
}

// GetEdges mirrors TableFinder.get_edges with default settings
// (lines/lines strategies, snap/join 3, prefilter 1, min length 3).
func GetEdges(p pdftext.Page) []Edge {
	all := PageEdges(p)
	v := FilterEdges(all, 'v', EdgeMinLengthPrefilter)
	h := FilterEdges(all, 'h', EdgeMinLengthPrefilter)
	edges := append(append([]Edge(nil), v...), h...)
	edges = mergeEdges(edges, SnapTolerance, SnapTolerance, JoinTolerance, JoinTolerance)
	return FilterEdges(edges, 0, EdgeMinLength)
}

// Point is an intersection vertex.
type Point [2]float64

// Intersections maps a vertex to the touching v/h edges.
type Intersections map[Point]struct {
	V []Edge
	H []Edge
}

// EdgesToIntersections mirrors pdfplumber table.edges_to_intersections.
func EdgesToIntersections(edges []Edge, xTol, yTol float64) Intersections {
	var vs, hs []Edge
	for _, e := range edges {
		if e.Orient == 'v' {
			vs = append(vs, e)
		} else {
			hs = append(hs, e)
		}
	}
	sort.SliceStable(vs, func(a, b int) bool {
		if vs[a].X0 != vs[b].X0 {
			return vs[a].X0 < vs[b].X0
		}
		return vs[a].Top < vs[b].Top
	})
	sort.SliceStable(hs, func(a, b int) bool {
		if hs[a].Top != hs[b].Top {
			return hs[a].Top < hs[b].Top
		}
		return hs[a].X0 < hs[b].X0
	})
	out := Intersections{}
	for _, v := range vs {
		for _, h := range hs {
			if v.Top <= h.Top+yTol && v.Bottom >= h.Top-yTol &&
				v.X0 >= h.X0-xTol && v.X0 <= h.X1+xTol {
				pt := Point{v.X0, h.Top}
				en := out[pt]
				en.V = append(en.V, v)
				en.H = append(en.H, h)
				out[pt] = en
			}
		}
	}
	return out
}

func edgeBBox(e Edge) BBox { return BBox{e.X0, e.Top, e.X1, e.Bottom} }

// IntersectionsToCells mirrors pdfplumber table.intersections_to_cells.
func IntersectionsToCells(in Intersections) []BBox {
	edgeConnects := func(p1, p2 Point) bool {
		e1, ok1 := in[p1]
		e2, ok2 := in[p2]
		if !ok1 || !ok2 {
			return false
		}
		if p1[0] == p2[0] {
			set := map[BBox]struct{}{}
			for _, e := range e1.V {
				set[edgeBBox(e)] = struct{}{}
			}
			for _, e := range e2.V {
				if _, ok := set[edgeBBox(e)]; ok {
					return true
				}
			}
		}
		if p1[1] == p2[1] {
			set := map[BBox]struct{}{}
			for _, e := range e1.H {
				set[edgeBBox(e)] = struct{}{}
			}
			for _, e := range e2.H {
				if _, ok := set[edgeBBox(e)]; ok {
					return true
				}
			}
		}
		return false
	}
	points := make([]Point, 0, len(in))
	for p := range in {
		points = append(points, p)
	}
	sort.Slice(points, func(a, b int) bool {
		if points[a][0] != points[b][0] {
			return points[a][0] < points[b][0]
		}
		return points[a][1] < points[b][1]
	})
	n := len(points)
	var cells []BBox
	for i := 0; i < n; i++ {
		if i == n-1 {
			continue
		}
		pt := points[i]
		rest := points[i+1:]
		var below, right []Point
		for _, x := range rest {
			if x[0] == pt[0] {
				below = append(below, x)
			}
			if x[1] == pt[1] {
				right = append(right, x)
			}
		}
		found := false
		var cell BBox
		for _, bp := range below {
			if !edgeConnects(pt, bp) {
				continue
			}
			for _, rp := range right {
				if !edgeConnects(pt, rp) {
					continue
				}
				br := Point{rp[0], bp[1]}
				if _, ok := in[br]; ok && edgeConnects(br, rp) && edgeConnects(br, bp) {
					cell = BBox{pt[0], pt[1], br[0], br[1]}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			cells = append(cells, cell)
		}
	}
	return cells
}

// CellsToTables mirrors pdfplumber table.cells_to_tables (groups with more
// than one cell, sorted top-to-bottom-left-to-right).
func CellsToTables(cells []BBox) [][]BBox {
	corners := func(b BBox) [4]Point {
		return [4]Point{{b[0], b[1]}, {b[0], b[3]}, {b[2], b[1]}, {b[2], b[3]}}
	}
	remaining := append([]BBox(nil), cells...)
	var tables [][]BBox
	var curCorners map[Point]struct{}
	var cur []BBox
	flush := func() {
		if len(cur) > 0 {
			tables = append(tables, cur)
			cur = nil
			curCorners = nil
		}
	}
	for len(remaining) > 0 {
		initial := len(cur)
		var next []BBox
		for _, cell := range remaining {
			cc := corners(cell)
			if len(cur) == 0 {
				if curCorners == nil {
					curCorners = map[Point]struct{}{}
				}
				for _, c := range cc {
					curCorners[c] = struct{}{}
				}
				cur = append(cur, cell)
			} else {
				n := 0
				for _, c := range cc {
					if _, ok := curCorners[c]; ok {
						n++
					}
				}
				if n > 0 {
					for _, c := range cc {
						curCorners[c] = struct{}{}
					}
					cur = append(cur, cell)
				} else {
					next = append(next, cell)
				}
			}
		}
		remaining = next
		if len(cur) == initial {
			flush()
			// Start a new group with the first remaining cell on the
			// next iteration (mirrors the Python loop, which assigns
			// the first cell of the next pass to the empty group).
			if len(remaining) == 0 {
				break
			}
		}
	}
	flush()
	sort.Slice(tables, func(a, b int) bool {
		maxt, maxx := tables[a][0][1], tables[a][0][0]
		for _, c := range tables[a] {
			if c[1] < maxt || (c[1] == maxt && c[0] < maxx) {
				maxt, maxx = c[1], c[0]
			}
		}
		mbt, mbx := tables[b][0][1], tables[b][0][0]
		for _, c := range tables[b] {
			if c[1] < mbt || (c[1] == mbt && c[0] < mbx) {
				mbt, mbx = c[1], c[0]
			}
		}
		if maxt != mbt {
			return maxt < mbt
		}
		return maxx < mbx
	})
	var filtered [][]BBox
	for _, t := range tables {
		if len(t) > 1 {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// Table is one detected table: its cells plus the page chars for text.
type Table struct {
	Cells []BBox
	Chars []pdftext.Char
}

// FindTables runs the full default pipeline on one page.
func FindTables(p pdftext.Page) []Table {
	edges := GetEdges(p)
	inter := EdgesToIntersections(edges, IntersectionTolerance, IntersectionTolerance)
	cells := IntersectionsToCells(inter)
	var out []Table
	for _, group := range CellsToTables(cells) {
		out = append(out, Table{Cells: group, Chars: p.Chars})
	}
	return out
}

// Row is one table row: cells left-to-right, nil for grid holes.
type Row struct {
	Cells []*BBox
	BBox  BBox // over non-nil cells
}

// Rows mirrors Table.rows (_get_rows_or_cols with Row).
func (t Table) Rows() []Row {
	// Sort by (top, x0).
	sorted := append([]BBox(nil), t.Cells...)
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a][1] != sorted[b][1] {
			return sorted[a][1] < sorted[b][1]
		}
		return sorted[a][0] < sorted[b][0]
	})
	xset := map[float64]struct{}{}
	for _, c := range t.Cells {
		xset[c[0]] = struct{}{}
	}
	xs := make([]float64, 0, len(xset))
	for x := range xset {
		xs = append(xs, x)
	}
	sort.Float64s(xs)
	var rows []Row
	for i := 0; i < len(sorted); {
		j := i + 1
		for j < len(sorted) && sorted[j][1] == sorted[i][1] {
			j++
		}
		xdict := map[float64]BBox{}
		for _, c := range sorted[i:j] {
			xdict[c[0]] = c
		}
		var cells []*BBox
		var bbox BBox
		first := true
		for _, x := range xs {
			if c, ok := xdict[x]; ok {
				cc := c
				cells = append(cells, &cc)
				if first {
					bbox = c
					first = false
				} else {
					if c[0] < bbox[0] {
						bbox[0] = c[0]
					}
					if c[1] < bbox[1] {
						bbox[1] = c[1]
					}
					if c[2] > bbox[2] {
						bbox[2] = c[2]
					}
					if c[3] > bbox[3] {
						bbox[3] = c[3]
					}
				}
			} else {
				cells = append(cells, nil)
			}
		}
		rows = append(rows, Row{Cells: cells, BBox: bbox})
		i = j
	}
	return rows
}

func charInBBox(ch pdftext.Char, b BBox) bool {
	vmid := (ch.Top + ch.Bottom) / 2
	hmid := (ch.X0 + ch.X1) / 2
	return hmid >= b[0] && hmid < b[2] && vmid >= b[1] && vmid < b[3]
}

// Extract mirrors Table.extract with default text settings: per row, per
// cell, the cell text or "" (no chars) or nil (grid hole).
func (t Table) Extract() [][]*string {
	rows := t.Rows()
	out := make([][]*string, 0, len(rows))
	for _, row := range rows {
		var rowChars []pdftext.Char
		for _, ch := range t.Chars {
			if charInBBox(ch, row.BBox) {
				rowChars = append(rowChars, ch)
			}
		}
		arr := make([]*string, 0, len(row.Cells))
		for _, cell := range row.Cells {
			if cell == nil {
				arr = append(arr, nil)
				continue
			}
			var cellChars []pdftext.Char
			for _, ch := range rowChars {
				if charInBBox(ch, *cell) {
					cellChars = append(cellChars, ch)
				}
			}
			if len(cellChars) == 0 {
				s := ""
				arr = append(arr, &s)
			} else {
				s := ExtractText(cellChars)
				arr = append(arr, &s)
			}
		}
		out = append(out, arr)
	}
	return out
}

// ExtractTables mirrors Page.extract_tables with default settings.
func ExtractTables(p pdftext.Page) [][][]*string {
	var out [][][]*string
	for _, t := range FindTables(p) {
		out = append(out, t.Extract())
	}
	return out
}
