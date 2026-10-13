// internal/entity/strategy_history.go 定義永久保存的整日決策紀錄與索引資料。
package entity

// StrategyHistoryRecord 保存不可變的快照本文及可直接查詢的摘要。
type StrategyHistoryRecord struct {
	Revision                                                                       string
	DecisionDate, Source, ExecutionMode, RecordedAt, StrategyVersion, Availability string
	Snapshot                                                                       string
}
