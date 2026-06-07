// internal/entity/ledger.go 定義投資組合 ledger 資料表的資料模型。
package entity

// UnrealizedGainsLoss 對應 UnrealizedGainsLosses table,PK (transaction_date, stock_id)。
// 同時取代舊的 sqls.LotRecord 與 GetLowestUnrealizedGainsLossesRecord 回傳的
// map[string]interface{} —— 上線 engine 重啟還原持倉、賣出 lot-matching 都以此型別流通。
type UnrealizedGainsLoss struct {
	TransactionDate  string  // transaction_date  VARCHAR(50) NOT NULL
	StockID          string  // stock_id          VARCHAR(10) NOT NULL
	StockName        string  // stock_name        VARCHAR(50) NOT NULL
	TransactionPrice float64 // transaction_price DECIMAL(10,2) NOT NULL
	InvestmentCost   float64 // investment_cost   DECIMAL(12,2) NOT NULL
	Shares           int     // shares            INT NOT NULL DEFAULT 0
}

// RealizedGainsLoss 對應 RealizedGainsLosses table,PK (stock_id, buy_date, sell_date)。
type RealizedGainsLoss struct {
	BuyDate        string  // buy_date        DATE NOT NULL
	SellDate       string  // sell_date       DATE NOT NULL
	StockID        string  // stock_id        VARCHAR(10) NOT NULL
	StockName      string  // stock_name      VARCHAR(50) NOT NULL
	PurchasePrice  float64 // purchase_price  DECIMAL(10,2) NOT NULL
	SellPrice      float64 // sell_price      DECIMAL(10,2) NOT NULL
	InvestmentCost float64 // investment_cost DECIMAL(12,2) NOT NULL
	Revenue        float64 // revenue         DECIMAL(12,2) NOT NULL
	ProfitLoss     float64 // profit_loss     DECIMAL(12,2) NOT NULL
	ProfitRate     float64 // profit_rate     DECIMAL(10,2) NOT NULL
	Shares         int     // shares          INT NOT NULL DEFAULT 0
}
