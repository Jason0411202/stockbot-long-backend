package kernals

import (
	"fmt"
	"main/app_context"
	"main/config"
	"math"
	"sort"
	"time"
)

// BacktestResult 是一次回測的數值結果。衡量指標為 FinalTotal = FinalCash + FinalHoldingValue。
type BacktestResult struct {
	InitialCash       float64
	FinalCash         float64
	FinalHoldingValue float64
	FinalTotal        float64
	TotalBuys         int
	TotalSells        int
	SkippedBuys       int // 因現金不足而被跳過的買入次數 (防作弊夾取)
}

// lot 為記憶體中的單筆未實現持倉。
type lot struct {
	date   time.Time
	shares int
	price  float64 // 買入單價
}

// stockSeries 為單一股票經整理後的歷史資料。
type stockSeries struct {
	dates       []time.Time // asc
	dateIndex   map[string]int
	closePrices []float64
	ma20        []float64 // ma20[i] = 截至 dates[i] 的 20 日均價；不足 20 日以 NaN 表示
}

// RunBacktest 以記憶體為主的方式，對所有追蹤股票做一次回測。
// 回測邏輯與 BuyStock/SellStock 相同，但省略大量 DB 來回與 Discord 通知。
func RunBacktest(appCtx *app_context.AppContext, backTestDays int) (*BacktestResult, error) {
	if appCtx.Cfg.ScalingStrategy != "Baseline" {
		return nil, fmt.Errorf("回測目前僅支援 Scaling_Strategy=Baseline")
	}
	series, err := loadStockSeries(appCtx)
	if err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return nil, fmt.Errorf("無任何股票歷史資料可供回測")
	}
	return runBacktestOnSeries(appCtx.Cfg, series, backTestDays)
}

// runBacktestOnSeries 為不依賴 DB 與 Discord 的純函式版本，方便做單元測試。
// 唯一交易策略為 baseline method (原金字塔策略)，並實作「現金不得為負」的夾取。
func runBacktestOnSeries(cfg *config.Config, series map[string]*stockSeries, backTestDays int) (*BacktestResult, error) {
	if cfg.ScalingStrategy != "Baseline" {
		return nil, fmt.Errorf("回測目前僅支援 Scaling_Strategy=Baseline")
	}

	allDates := collectDateUnion(series)
	if len(allDates) == 0 {
		return nil, fmt.Errorf("無任何日期可供回測")
	}
	if backTestDays > 0 && backTestDays < len(allDates) {
		allDates = allDates[len(allDates)-backTestDays:]
	}

	cash := cfg.InitialCash
	positions := make(map[string][]lot, len(cfg.TrackStocks))
	lastBuy := make(map[string]time.Time, len(cfg.TrackStocks))
	cooldown := time.Duration(cfg.CooldownDays) * 24 * time.Hour
	mult := cfg.BuyAndSellMultiplier
	sellAmountTarget := cfg.BaselineSellAmount
	sellThreshold := cfg.BaselineSellThreshold

	totalBuys := 0
	totalSells := 0
	skippedBuys := 0

	for _, today := range allDates {
		for _, stockID := range cfg.TrackStocks {
			s, ok := series[stockID]
			if !ok {
				continue
			}
			idx, ok := s.dateIndex[today.Format("2006-01-02")]
			if !ok {
				continue
			}
			todayPrice := s.closePrices[idx]
			if todayPrice <= 0 {
				continue
			}

			// === 買入判斷 ===
			ma20 := s.ma20[idx]
			if !math.IsNaN(ma20) && todayPrice < ma20 {
				lb, hasLastBuy := lastBuy[stockID]
				if !hasLastBuy || today.Sub(lb) >= cooldown {
					highestPrice := -1.0
					for _, l := range positions[stockID] {
						if l.price > highestPrice {
							highestPrice = l.price
						}
					}
					percentages := 0.0
					if highestPrice > 0 {
						percentages = (todayPrice - highestPrice) / highestPrice
					}
					buyAmount := baselineBuyAmountFromCfg(cfg, percentages) * mult
					shares := amountToShares(buyAmount, todayPrice)

					// === 防作弊現金夾取：可利用資金僅為當前持有現金，不得借錢 ===
					// 依當下可用現金限縮買入股數；若一股都買不起則跳過 (不扣負數現金)。
					maxAffordable := 0
					if todayPrice > 0 {
						maxAffordable = int(math.Floor(cash / todayPrice))
					}
					if shares > maxAffordable {
						shares = maxAffordable
					}
					if shares <= 0 {
						// 略過此次買進，記錄一次被夾取的訊號
						if buyAmount > 0 {
							skippedBuys++
						}
					} else {
						cost := float64(shares) * todayPrice
						cash -= cost
						if cash < 0 {
							// 不變量被破壞：任何路徑都不得讓現金為負。
							return nil, fmt.Errorf("internal invariant violated: cash went negative (%.6f)", cash)
						}
						positions[stockID] = append(positions[stockID], lot{
							date:   today,
							shares: shares,
							price:  todayPrice,
						})
						lastBuy[stockID] = today
						totalBuys++
					}
				}
			}

			// === 賣出判斷 ===
			pos := positions[stockID]
			if len(pos) == 0 {
				continue
			}
			lowestPrice := math.MaxFloat64
			for _, l := range pos {
				if l.price < lowestPrice {
					lowestPrice = l.price
				}
			}
			if lowestPrice <= 0 {
				continue
			}
			gain := (todayPrice - lowestPrice) / lowestPrice
			if gain < sellThreshold {
				continue
			}
			targetShares := amountToShares(sellAmountTarget*mult, todayPrice)
			if targetShares <= 0 {
				continue
			}

			// 從成本最低的 lot 開始賣
			sort.SliceStable(pos, func(i, j int) bool {
				if pos[i].price != pos[j].price {
					return pos[i].price < pos[j].price
				}
				return pos[i].date.Before(pos[j].date)
			})
			remaining := targetShares
			newPos := pos[:0]
			for _, l := range pos {
				if remaining <= 0 {
					newPos = append(newPos, l)
					continue
				}
				if l.shares <= remaining {
					cash += float64(l.shares) * todayPrice
					remaining -= l.shares
					totalSells++
				} else {
					cash += float64(remaining) * todayPrice
					l.shares -= remaining
					remaining = 0
					totalSells++
					newPos = append(newPos, l)
				}
			}
			positions[stockID] = newPos
		}

		if cash < 0 {
			return nil, fmt.Errorf("internal invariant violated: cash went negative (%.6f) on %s", cash, today.Format("2006-01-02"))
		}
	}

	// 計算期末持股市值：每檔股票以其最後可得收盤價結算。
	finalHolding := 0.0
	for stockID, pos := range positions {
		if len(pos) == 0 {
			continue
		}
		s := series[stockID]
		lastPrice := s.closePrices[len(s.closePrices)-1]
		for _, l := range pos {
			finalHolding += float64(l.shares) * lastPrice
		}
	}

	return &BacktestResult{
		InitialCash:       cfg.InitialCash,
		FinalCash:         cash,
		FinalHoldingValue: finalHolding,
		FinalTotal:        cash + finalHolding,
		TotalBuys:         totalBuys,
		TotalSells:        totalSells,
		SkippedBuys:       skippedBuys,
	}, nil
}

// baselineBuyAmountFromCfg 依照 config 中 tier 決定買入目標金額，純函式版。
func baselineBuyAmountFromCfg(cfg *config.Config, percentages float64) float64 {
	for _, tier := range cfg.BaselineBuyTiers {
		if percentages > tier.Above {
			return tier.Amount
		}
	}
	return cfg.BaselineBuyFallbackAmount
}

// loadStockSeries 從 DB 一次讀入所有追蹤股票的歷史資料並預先計算 20MA。
func loadStockSeries(appCtx *app_context.AppContext) (map[string]*stockSeries, error) {
	series := make(map[string]*stockSeries, len(appCtx.Cfg.TrackStocks))

	for _, stockID := range appCtx.Cfg.TrackStocks {
		rows, err := appCtx.Db.Query("SELECT date, close_price FROM StockHistory WHERE stock_id = ? ORDER BY date ASC;", stockID)
		if err != nil {
			return nil, fmt.Errorf("load %s history: %w", stockID, err)
		}

		dates := make([]time.Time, 0, 2048)
		prices := make([]float64, 0, 2048)
		for rows.Next() {
			var dateStr string
			var price float64
			if err := rows.Scan(&dateStr, &price); err != nil {
				rows.Close()
				return nil, err
			}
			// date 欄位存在多種格式 (VARCHAR)；優先嘗試 "2006-01-02"，退而求其次不切 time，只用字串比較。
			t, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				// 如果 VARCHAR 帶時間，嘗試其他格式
				t, err = time.Parse("2006-01-02 15:04:05", dateStr)
				if err != nil {
					continue
				}
			}
			dates = append(dates, t)
			prices = append(prices, price)
		}
		rows.Close()

		if len(dates) == 0 {
			appCtx.Log.Warn("無歷史資料 stockID=", stockID)
			continue
		}

		idx := make(map[string]int, len(dates))
		for i, d := range dates {
			idx[d.Format("2006-01-02")] = i
		}

		ma20 := make([]float64, len(dates))
		const window = 20
		sum := 0.0
		for i, p := range prices {
			sum += p
			if i >= window {
				sum -= prices[i-window]
			}
			if i >= window-1 {
				ma20[i] = sum / float64(window)
			} else {
				ma20[i] = math.NaN()
			}
		}

		series[stockID] = &stockSeries{
			dates:       dates,
			dateIndex:   idx,
			closePrices: prices,
			ma20:        ma20,
		}
	}

	return series, nil
}

// collectDateUnion 回傳所有股票日期的聯集，升冪排序。
func collectDateUnion(series map[string]*stockSeries) []time.Time {
	seen := make(map[string]struct{}, 2048)
	for _, s := range series {
		for _, d := range s.dates {
			seen[d.Format("2006-01-02")] = struct{}{}
		}
	}
	out := make([]time.Time, 0, len(seen))
	for k := range seen {
		t, err := time.Parse("2006-01-02", k)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
