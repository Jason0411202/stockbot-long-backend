// internal/entity/stock.go 定義股票歷史行情與 TWSE 原始 K 棒的資料模型。
package entity

// StockHistory 對應 StockHistory table,PK (stock_id, date)。
// 日期欄位以 string 保存 (DB 為 VARCHAR、已由 ROC 轉 AD 的 "YYYY-MM-DD"),
// time.Time 解析交由 service 層處理,避免與 VARCHAR date 欄位的 scan 摩擦。
type StockHistory struct {
	StockID      string  // stock_id      VARCHAR(10) NOT NULL
	StockName    string  // stock_name    VARCHAR(50)
	Date         string  // date          VARCHAR(50)  (YYYY-MM-DD)
	Volume       int     // volume        INT
	Value        int64   // value         BIGINT
	OpenPrice    float64 // open_price    DECIMAL(10,2)
	HighPrice    float64 // high_price    DECIMAL(10,2)
	LowPrice     float64 // low_price     DECIMAL(10,2)
	ClosePrice   float64 // close_price   DECIMAL(10,2)
	PriceChange  float64 // price_change  DECIMAL(10,2)
	Transactions int     // transactions  INT
}

// Bar 為單一交易日的 OHLCV,由 TWSE client 解析後回傳 (取代 cmd/fetch_data 的私有 bar
// 與 sqls.TWSEapi 的 [][]string 原始字串路徑)。Date 為 AD "YYYY-MM-DD"。
type Bar struct {
	Date   string  // 交易日期 (西元 YYYY-MM-DD)
	Open   float64 // 開盤價
	High   float64 // 最高價
	Low    float64 // 最低價
	Close  float64 // 收盤價
	Volume float64 // 成交量 (股)
}
