// xlsx_compat_test.go — XLSX rendering & Excel-environment compatibility
// verification for the EstateMuse topics.v1 export (APP-1065).
//
// The suite builds a *representative* export sample and asserts its
// structure by parsing the produced bytes back, in two layers:
//
//  1. Production path — a topics.v1 snapshot mirroring
//     templates/topics.xlsx.tmpl (500 rows, Chinese headers, dropdown
//     validation on 维度/切面/难度/建议产物, frozen header row, per-row
//     action hyperlinks) plus a second numeric "市场数据" sheet (int /
//     float cells, 3-color conditional formatting), rendered through the
//     kernel's pkg/artifact.XLSXRenderer — the exact code path
//     EstateMuse uses in production.
//
//  2. Compatibility post-processing — the XLSXRenderer API exposes no
//     merged-cell or explicit number-format controls (renderer
//     capability gap, recorded on APP-1065). To still exercise those
//     constructs against real spreadsheet apps, the sample is
//     post-processed with the same xuri/excelize/v2 writer the renderer
//     itself uses: a merged A14:D14 footer on 市场数据 and explicit
//     number formats (#,##0.00 on 均价, 0.0% on 环比).
//
// Structural assertions read the final workbook back with excelize;
// the AutoFilter (no read API in excelize v2.9.1) is asserted against
// the raw worksheet XML inside the xlsx zip container.
//
// Set E2E_WRITE_XLSX_SAMPLE=1 to also write the workbook to
// testdata/topics-compat-sample.xlsx — the archived sample used for
// manual rendering checks in desktop spreadsheet apps (Microsoft
// Excel / WPS / Numbers).
package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/soyaos/soyaos/pkg/artifact"
	"github.com/xuri/excelize/v2"
)

// topicsCompatRows is the number of data rows in the Topics sheet of the
// compatibility sample — matches the DD-010 headline scale ("a 500-row
// Excel of editorial-grade topic ideas").
const topicsCompatRows = 500

// topicsSeedRows are the 10 representative topics.v1 rows from
// examples/expected-topics-1.json: every dimension (8) and angle (5)
// enum value appears at least once, all cells are Chinese-heavy.
var topicsSeedRows = [][]any{
	{"亚运村次新房 2024 全年成交价柱状图", "market", "数据", "亚运村去年到底涨没涨，一张图说清楚", "med", "图文"},
	{"30 万首付能在亚运村买到什么样的房子", "buy", "案例", "30 万首付的亚运村购房清单", "low", "图文+短视频"},
	{"亚运村 vs 滨江区次新房价比", "compare", "对比", "同价位换板块，3 公里之差到底差在哪里", "med", "图文+短视频"},
	{"亚运村小区物业服务横评", "hold", "对比", "5 个亚运村小区物业打分", "low", "图文"},
	{"杭州限购对亚运村次新房的影响", "policy", "数据", "限购新政之后亚运村成交量变化", "high", "图文"},
	{"亚运村到武林广场通勤实测", "lifestyle", "案例", "早 8 点出门，9 点能否在武林开会", "low", "短视频"},
	{"亚运村二手房常见合同陷阱", "risk", "操作", "签合同前必看的 5 个陷阱条款", "med", "图文"},
	{"亚运村次新房卖出时机", "sell", "争议", "现在卖还是再等一年", "high", "图文"},
	{"亚运村首付门槛实测", "buy", "数据", "实测亚运村 12 个挂牌房的首付预算", "med", "图文"},
	{"亚运村学区房新政影响", "policy", "争议", "学区调整后哪些楼盘价值缩水", "high", "图文"},
}

// marketRows is the numeric 市场数据 sheet: month (string), 成交量 (int),
// 均价 (float, post-formatted #,##0.00), 环比 (float ratio, post-formatted
// 0.0%). Values are sample data, not real transactions.
var marketRows = [][]any{
	{"2024-01", 182, 30521.50, 0.0},
	{"2024-02", 96, 30102.00, -0.0137},
	{"2024-03", 240, 30876.25, 0.0257},
	{"2024-04", 211, 31002.80, 0.0041},
	{"2024-05", 198, 30654.10, -0.0112},
	{"2024-06", 226, 30988.00, 0.0109},
	{"2024-07", 173, 30410.40, -0.0186},
	{"2024-08", 165, 30287.90, -0.004},
	{"2024-09", 254, 31120.60, 0.0275},
	{"2024-10", 287, 31544.35, 0.0136},
	{"2024-11", 232, 31390.00, -0.0049},
	{"2024-12", 269, 31755.75, 0.0117},
}

const (
	marketFooterCellRange = "A14:D14"
	marketFooterText      = "数据来源：样例数据（非真实成交），用于 XLSX 兼容性验证 — APP-1065"
	perRowActionURLTmpl   = "https://soyaos.local/v1/agents/estate-muse/actions/generate_post?row_id={row_id}"
)

// buildCompatSnapshot assembles the representative topics.v1 snapshot:
// the Topics sheet exactly mirrors templates/topics.xlsx.tmpl (headers,
// widths, validations, freeze, per-row action URL) at 500-row scale; the
// 市场数据 sheet exercises the multi-sheet path with numeric cells and
// 3-color conditional formatting.
func buildCompatSnapshot() artifact.XLSXSnapshot {
	rows := make([][]any, 0, topicsCompatRows)
	for i := 0; i < topicsCompatRows; i++ {
		seed := topicsSeedRows[i%len(topicsSeedRows)]
		row := make([]any, len(seed))
		copy(row, seed)
		row[0] = fmt.Sprintf("%s（样本 %03d）", seed[0], i+1)
		rows = append(rows, row)
	}

	return artifact.XLSXSnapshot{
		Sheets: []artifact.XLSXSheet{
			{
				Name:            "Topics",
				FreezeHeader:    true,
				PerRowActionURL: perRowActionURLTmpl,
				Columns: []artifact.XLSXColumn{
					{Header: "标题", Width: 42},
					{Header: "维度", Width: 12, Validation: []string{"buy", "hold", "sell", "market", "policy", "lifestyle", "risk", "compare"}},
					{Header: "切面", Width: 10, Validation: []string{"数据", "案例", "对比", "操作", "争议"}},
					{Header: "钩子", Width: 36},
					{Header: "难度", Width: 8, Validation: []string{"low", "med", "high"}},
					{Header: "建议产物", Width: 14, Validation: []string{"图文", "短视频", "图文+短视频"}},
				},
				Rows: rows,
			},
			{
				Name:         "市场数据",
				FreezeHeader: true,
				Columns: []artifact.XLSXColumn{
					{Header: "月份", Width: 12},
					{Header: "成交量（套）", Width: 14},
					{Header: "均价（元/㎡）", Width: 16},
					{Header: "环比", Width: 10, Conditional: "3color"},
				},
				Rows: marketRows,
			},
		},
	}
}

// renderCompatWorkbook renders the snapshot through the production
// XLSXRenderer, then applies the compatibility post-processing layer
// (merged footer + explicit number formats) that the renderer API does
// not expose. Returns the final workbook bytes plus the renderer's
// artifact descriptor.
func renderCompatWorkbook(t *testing.T) ([]byte, artifact.Artifact) {
	t.Helper()

	// Layer 1 — production render path.
	var rendered bytes.Buffer
	art, err := artifact.XLSXRenderer{Schema: "topics.v1"}.Render(context.Background(), buildCompatSnapshot(), &rendered)
	if err != nil {
		t.Fatalf("XLSXRenderer.Render: %v", err)
	}

	// Layer 2 — compatibility post-processing with the renderer's own
	// writer library (merged cells + number formats; renderer gap, see
	// file header / APP-1065).
	f, err := excelize.OpenReader(bytes.NewReader(rendered.Bytes()))
	if err != nil {
		t.Fatalf("reopen rendered workbook: %v", err)
	}
	defer func() { _ = f.Close() }()

	const sheet = "市场数据"
	priceFmt := "#,##0.00"
	pctFmt := "0.0%"
	priceStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &priceFmt})
	if err != nil {
		t.Fatalf("build price style: %v", err)
	}
	pctStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &pctFmt})
	if err != nil {
		t.Fatalf("build percent style: %v", err)
	}
	lastDataRow := len(marketRows) + 1
	if err := f.SetCellStyle(sheet, "C2", fmt.Sprintf("C%d", lastDataRow), priceStyle); err != nil {
		t.Fatalf("apply #,##0.00 to 均价: %v", err)
	}
	if err := f.SetCellStyle(sheet, "D2", fmt.Sprintf("D%d", lastDataRow), pctStyle); err != nil {
		t.Fatalf("apply 0.0%% to 环比: %v", err)
	}

	parts := strings.SplitN(marketFooterCellRange, ":", 2)
	if err := f.SetCellValue(sheet, parts[0], marketFooterText); err != nil {
		t.Fatalf("set merged footer text: %v", err)
	}
	if err := f.MergeCell(sheet, parts[0], parts[1]); err != nil {
		t.Fatalf("merge footer %s: %v", marketFooterCellRange, err)
	}

	var final bytes.Buffer
	if _, err := f.WriteTo(&final); err != nil {
		t.Fatalf("write post-processed workbook: %v", err)
	}
	return final.Bytes(), art
}

// compatWorkbook memoises the rendered sample across the suite.
var compatWorkbook = struct {
	once sync.Once
	data []byte
	art  artifact.Artifact
}{}

func compatSample(t *testing.T) ([]byte, artifact.Artifact) {
	t.Helper()
	compatWorkbook.once.Do(func() {
		compatWorkbook.data, compatWorkbook.art = renderCompatWorkbook(t)
	})
	if len(compatWorkbook.data) == 0 {
		t.Fatal("compat sample render failed in earlier test")
	}
	return compatWorkbook.data, compatWorkbook.art
}

func openCompatSample(t *testing.T) *excelize.File {
	t.Helper()
	data, _ := compatSample(t)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("excelize.OpenReader on final sample: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestXLSXCompat_Render_ProducesValidWorkbook verifies the production
// render succeeds at 500-row scale, stamps the artifact descriptor, and
// produces bytes a spreadsheet library can re-open.
func TestXLSXCompat_Render_ProducesValidWorkbook(t *testing.T) {
	data, art := compatSample(t)

	if art.Kind != artifact.KindXLSX {
		t.Errorf("artifact kind = %v, want %v", art.Kind, artifact.KindXLSX)
	}
	if art.Schema != "topics.v1" {
		t.Errorf("artifact schema = %q, want topics.v1", art.Schema)
	}
	if art.MIMEType != artifact.XLSXMIME {
		t.Errorf("artifact MIME = %q, want %q", art.MIMEType, artifact.XLSXMIME)
	}
	if art.Size <= 0 {
		t.Errorf("artifact size = %d, want > 0", art.Size)
	}

	f := openCompatSample(t)
	want := []string{"Topics", "市场数据"}
	got := f.GetSheetList()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sheet list = %v, want %v", got, want)
	}
	if len(data) == 0 {
		t.Fatal("empty workbook bytes")
	}
}

// TestXLSXCompat_TopicsSheet_StructureMatchesTemplate asserts the Topics
// sheet against the templates/topics.xlsx.tmpl contract: Chinese
// headers, column widths, 500 data rows with intact Chinese content,
// frozen header pane, dropdown validations, per-row action hyperlinks,
// and the AutoFilter region.
func TestXLSXCompat_TopicsSheet_StructureMatchesTemplate(t *testing.T) {
	f := openCompatSample(t)
	const sheet = "Topics"

	// Header row — Chinese headers survive the round trip byte-exact.
	wantHeaders := []string{"标题", "维度", "切面", "钩子", "难度", "建议产物"}
	for i, want := range wantHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		got, err := f.GetCellValue(sheet, cell)
		if err != nil {
			t.Fatalf("GetCellValue %s: %v", cell, err)
		}
		if got != want {
			t.Errorf("header %s = %q, want %q", cell, got, want)
		}
	}

	// Row count: header + 500 data rows.
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows) != topicsCompatRows+1 {
		t.Fatalf("row count = %d, want %d", len(rows), topicsCompatRows+1)
	}

	// First and last data rows carry intact Chinese content; titles are
	// shared strings (text cells), not numbers.
	a2, _ := f.GetCellValue(sheet, "A2")
	if want := "亚运村次新房 2024 全年成交价柱状图（样本 001）"; a2 != want {
		t.Errorf("A2 = %q, want %q", a2, want)
	}
	if ct, _ := f.GetCellType(sheet, "A2"); ct != excelize.CellTypeSharedString {
		t.Errorf("A2 cell type = %v, want shared string (text)", ct)
	}
	lastCell := fmt.Sprintf("A%d", topicsCompatRows+1)
	aLast, _ := f.GetCellValue(sheet, lastCell)
	if !strings.HasSuffix(aLast, "（样本 500）") {
		t.Errorf("%s = %q, want suffix （样本 500）", lastCell, aLast)
	}
	f2, _ := f.GetCellValue(sheet, "F2")
	if f2 != "图文" {
		t.Errorf("F2 = %q, want 图文", f2)
	}

	// Column widths as declared in the template.
	wantWidths := map[string]float64{"A": 42, "B": 12, "C": 10, "D": 36, "E": 8, "F": 14}
	for col, want := range wantWidths {
		got, err := f.GetColWidth(sheet, col)
		if err != nil {
			t.Fatalf("GetColWidth %s: %v", col, err)
		}
		if diff := got - want; diff < -0.5 || diff > 0.5 {
			t.Errorf("col %s width = %v, want ≈%v", col, got, want)
		}
	}

	// Frozen header row.
	panes, err := f.GetPanes(sheet)
	if err != nil {
		t.Fatalf("GetPanes: %v", err)
	}
	if !panes.Freeze || panes.YSplit != 1 || panes.TopLeftCell != "A2" {
		t.Errorf("panes = %+v, want frozen header (YSplit=1, TopLeftCell=A2)", panes)
	}

	// Dropdown validations on 维度 / 切面 / 难度 / 建议产物, including
	// Chinese enum values, spanning the full data region.
	wantValidations := map[string][]string{
		"B": {"buy", "hold", "sell", "market", "policy", "lifestyle", "risk", "compare"},
		"C": {"数据", "案例", "对比", "操作", "争议"},
		"E": {"low", "med", "high"},
		"F": {"图文", "短视频", "图文+短视频"},
	}
	dvs, err := f.GetDataValidations(sheet)
	if err != nil {
		t.Fatalf("GetDataValidations: %v", err)
	}
	seen := map[string]bool{}
	for _, dv := range dvs {
		col := strings.SplitN(dv.Sqref, ":", 2)[0][:1]
		want, ok := wantValidations[col]
		if !ok {
			t.Errorf("unexpected validation on %s", dv.Sqref)
			continue
		}
		seen[col] = true
		wantRange := fmt.Sprintf("%s2:%s%d", col, col, topicsCompatRows+1)
		if dv.Sqref != wantRange {
			t.Errorf("validation range on %s = %q, want %q", col, dv.Sqref, wantRange)
		}
		for _, v := range want {
			if !strings.Contains(dv.Formula1, v) {
				t.Errorf("validation %s droplist %q missing value %q", col, dv.Formula1, v)
			}
		}
	}
	for col := range wantValidations {
		if !seen[col] {
			t.Errorf("column %s has no dropdown validation", col)
		}
	}

	// Per-row action hyperlinks with substituted 1-based row_id.
	for _, probe := range []struct {
		cell string
		row  int
	}{{"A2", 1}, {"A258", 257}, {fmt.Sprintf("A%d", topicsCompatRows+1), topicsCompatRows}} {
		ok, link, err := f.GetCellHyperLink(sheet, probe.cell)
		if err != nil {
			t.Fatalf("GetCellHyperLink %s: %v", probe.cell, err)
		}
		want := strings.ReplaceAll(perRowActionURLTmpl, "{row_id}", strconv.Itoa(probe.row))
		if !ok || link != want {
			t.Errorf("hyperlink %s = (%v, %q), want (true, %q)", probe.cell, ok, link, want)
		}
	}

	// AutoFilter across header+data — excelize v2.9.1 has no read API,
	// assert against the raw worksheet XML in the zip container.
	assertAutoFilter(t, fmt.Sprintf("$A$1:$F$%d", topicsCompatRows+1))
}

// TestXLSXCompat_MarketSheet_NumbersMergesAndFormats covers the numeric /
// merged-cell / number-format compatibility surface on 市场数据.
func TestXLSXCompat_MarketSheet_NumbersMergesAndFormats(t *testing.T) {
	f := openCompatSample(t)
	const sheet = "市场数据"

	// Numeric cells: stored as numbers (not shared strings) and parseable
	// from the raw value.
	for _, cell := range []string{"B2", "C2", "D5", "B13", "C13", "D13"} {
		ct, err := f.GetCellType(sheet, cell)
		if err != nil {
			t.Fatalf("GetCellType %s: %v", cell, err)
		}
		if ct == excelize.CellTypeSharedString || ct == excelize.CellTypeInlineString {
			t.Errorf("%s stored as text, want numeric", cell)
		}
		raw, err := f.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatalf("GetCellValue raw %s: %v", cell, err)
		}
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			t.Errorf("%s raw value %q is not numeric: %v", cell, raw, err)
		}
	}

	// Number formats round-trip through excelize's formatter: 均价 uses
	// #,##0.00, 环比 uses 0.0%.
	if got, _ := f.GetCellValue(sheet, "C2"); got != "30,521.50" {
		t.Errorf("C2 formatted = %q, want 30,521.50 (#,##0.00)", got)
	}
	if got, _ := f.GetCellValue(sheet, "D3"); got != "-1.4%" {
		t.Errorf("D3 formatted = %q, want -1.4%% (0.0%%)", got)
	}
	if got, _ := f.GetCellValue(sheet, "D5"); got != "0.4%" {
		t.Errorf("D5 formatted = %q, want 0.4%% (0.0%%)", got)
	}

	// Merged footer region with intact Chinese content.
	merges, err := f.GetMergeCells(sheet)
	if err != nil {
		t.Fatalf("GetMergeCells: %v", err)
	}
	if len(merges) != 1 {
		t.Fatalf("merge count = %d, want 1 (%v)", len(merges), merges)
	}
	parts := strings.SplitN(marketFooterCellRange, ":", 2)
	if got := merges[0].GetStartAxis() + ":" + merges[0].GetEndAxis(); got != marketFooterCellRange {
		t.Errorf("merge range = %q, want %q", got, marketFooterCellRange)
	}
	if got := merges[0].GetCellValue(); got != marketFooterText {
		t.Errorf("merged cell value = %q, want %q", got, marketFooterText)
	}
	if got, _ := f.GetCellValue(sheet, parts[0]); got != marketFooterText {
		t.Errorf("footer %s = %q, want %q", parts[0], got, marketFooterText)
	}

	// 3-color conditional formatting on the 环比 data range.
	cfs, err := f.GetConditionalFormats(sheet)
	if err != nil {
		t.Fatalf("GetConditionalFormats: %v", err)
	}
	wantRange := fmt.Sprintf("D2:D%d", len(marketRows)+1)
	opts, ok := cfs[wantRange]
	if !ok || len(opts) == 0 {
		t.Fatalf("no conditional format on %s (got ranges %v)", wantRange, keysOf(cfs))
	}
	if opts[0].Type != "3_color_scale" {
		t.Errorf("conditional type = %q, want 3_color_scale", opts[0].Type)
	}

	// Frozen header on the second sheet too.
	panes, err := f.GetPanes(sheet)
	if err != nil {
		t.Fatalf("GetPanes: %v", err)
	}
	if !panes.Freeze || panes.YSplit != 1 {
		t.Errorf("panes = %+v, want frozen header", panes)
	}
}

// TestXLSXCompat_WriteSampleArchive writes the workbook to
// testdata/topics-compat-sample.xlsx for manual rendering checks in
// desktop spreadsheet apps. Gated behind E2E_WRITE_XLSX_SAMPLE=1 because
// xlsx containers embed timestamps and the bytes are not reproducible —
// unconditional writes would dirty the checkout on every run.
func TestXLSXCompat_WriteSampleArchive(t *testing.T) {
	if os.Getenv("E2E_WRITE_XLSX_SAMPLE") != "1" {
		t.Skip("set E2E_WRITE_XLSX_SAMPLE=1 to (re)write testdata/topics-compat-sample.xlsx")
	}
	data, _ := compatSample(t)
	path := filepath.Join("testdata", "topics-compat-sample.xlsx")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	t.Logf("wrote %s (%d bytes)", path, len(data))
}

// assertAutoFilter checks that some worksheet inside the workbook zip
// declares an <autoFilter> with exactly the wanted ref (excelize writes
// the ref in absolute form, e.g. $A$1:$F$501).
func assertAutoFilter(t *testing.T, wantRef string) {
	t.Helper()
	data, _ := compatSample(t)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open xlsx zip: %v", err)
	}
	needle := fmt.Sprintf(`<autoFilter ref="%s"`, wantRef)
	for _, zf := range zr.File {
		if !strings.HasPrefix(zf.Name, "xl/worksheets/") || !strings.HasSuffix(zf.Name, ".xml") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			t.Fatalf("open %s: %v", zf.Name, err)
		}
		xml, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", zf.Name, err)
		}
		if strings.Contains(string(xml), needle) {
			return
		}
	}
	t.Errorf("no worksheet declares autoFilter ref=%q", wantRef)
}

func keysOf[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
