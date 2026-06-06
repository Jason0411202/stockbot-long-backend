package kernals

import (
	"os"
	"path/filepath"
	"testing"
)

// datacache_test.go 驗證離線 CSV 載入路徑 (walk-forward 掃描用,不依賴 DB):
// header 略過、壞列略過、亂序重排、分割還原、缺檔報錯。

func writeCSV(t *testing.T, dir, stockID, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stockID+".csv"), []byte(body), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
}

func TestLoadSeriesFromCSV_ParsesAndSkips(t *testing.T) {
	// Arrange — 含 header、一條壞列 (欄位不足)、一條收盤<=0 列;其餘有效。
	dir := t.TempDir()
	writeCSV(t, dir, "AAA", `date,open,high,low,close,volume
2020-01-01,10,11,9,10,1000
2020-01-02,10,12,9,11,2000
bad,row,only,three
2020-01-03,11,13,10,0,3000
2020-01-04,11,14,10,12,4000
`)

	// Act
	series, err := LoadSeriesFromCSV(dir, []string{"AAA"})
	if err != nil {
		t.Fatalf("LoadSeriesFromCSV: %v", err)
	}

	// Assert — 只有 3 條有效列 (header/壞列/0 收盤被略過)。
	s := series["AAA"]
	if len(s.dates) != 3 {
		t.Fatalf("valid rows = %d, want 3 (closes %v)", len(s.dates), s.closePrices)
	}
	if _, ok := s.dateIndex["2020-01-04"]; !ok {
		t.Fatalf("expected 2020-01-04 indexed")
	}
}

func TestLoadSeriesFromCSV_SortsUnordered(t *testing.T) {
	// Arrange — 故意亂序;載入後應升冪。
	dir := t.TempDir()
	writeCSV(t, dir, "BBB", `date,open,high,low,close,volume
2020-03-01,1,1,1,30,1
2020-01-01,1,1,1,10,1
2020-02-01,1,1,1,20,1
`)

	// Act
	series, err := LoadSeriesFromCSV(dir, []string{"BBB"})
	if err != nil {
		t.Fatalf("LoadSeriesFromCSV: %v", err)
	}

	// Assert — 日期升冪、收盤隨日期同步重排。
	s := series["BBB"]
	for i := 1; i < len(s.dates); i++ {
		if !s.dates[i].After(s.dates[i-1]) {
			t.Fatalf("dates not ascending at %d", i)
		}
	}
	if s.closePrices[0] != 10 || s.closePrices[2] != 30 {
		t.Fatalf("closes not reordered with dates: %v", s.closePrices)
	}
}

func TestLoadSeriesFromCSV_MissingFileErrors(t *testing.T) {
	// Arrange + Act + Assert — 缺檔即回錯。
	if _, err := LoadSeriesFromCSV(t.TempDir(), []string{"NOPE"}); err == nil {
		t.Fatalf("expected error for missing CSV")
	}
}

func TestLoadSeriesFromCSV_EmptyFileErrors(t *testing.T) {
	// Arrange — 只有 header,無有效資料列。
	dir := t.TempDir()
	writeCSV(t, dir, "EMPTY", "date,open,high,low,close,volume\n")

	// Act + Assert
	if _, err := LoadSeriesFromCSV(dir, []string{"EMPTY"}); err == nil {
		t.Fatalf("expected error for file with no valid rows")
	}
}

func TestParseFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"12.5", 12.5},
		{"  7 ", 7},
		{"", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		if got := parseFloat(c.in); got != c.want {
			t.Fatalf("parseFloat(%q) = %g, want %g", c.in, got, c.want)
		}
	}
}

func TestEvaluateWalkForward_DelegatesToCore(t *testing.T) {
	// Arrange
	start := mustDate(t, "2019-01-01")
	series := map[string]*stockSeries{
		"A": seriesFrom(start, constPrices(800, 100)),
		"B": seriesFrom(start, constPrices(800, 100)),
	}
	cfg := baseCfg("A", "B")
	p := WalkForwardParams{WindowMonths: 12, StepMonths: 6, MinTradeDays: 100}

	// Act — 匯出包裝應與內部核心結果一致。
	_, aggWrap, err := EvaluateWalkForward(cfg, series, p)
	if err != nil {
		t.Fatalf("EvaluateWalkForward: %v", err)
	}
	_, aggCore, err := walkForwardOnSeries(cfg, series, p)
	if err != nil {
		t.Fatalf("walkForwardOnSeries: %v", err)
	}

	// Assert
	if aggWrap.NWindows != aggCore.NWindows {
		t.Fatalf("wrapper NWindows = %d, core = %d", aggWrap.NWindows, aggCore.NWindows)
	}
}
