// internal/service/equity_service.go 組裝實盤每日權益歷史 API 回應 (供前端歷史權益折線圖)。
package service

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/Jason0411202/stockbot-long-backend/internal/dto"
	"github.com/Jason0411202/stockbot-long-backend/internal/entity"
)

// equityHistoryReader 是 EquityHistoryService 取得每日權益快照的最小讀取介面 (由 EquityStore / repository 滿足)。
type equityHistoryReader interface {
	ListEquityAsc(ctx context.Context) ([]entity.EquitySnapshot, error)
}

// EquityHistoryService 讀取實盤每日權益快照 (EquityHistory) 並映射成 API DTO,供前端繪製歷史權益折線圖。
// 長序列以等距取樣壓縮 (與回測 equity_curve 同上限),控制回應大小;資料隨上線運行逐日累積。
type EquityHistoryService struct {
	equity equityHistoryReader
	log    *logrus.Logger
}

// NewEquityHistoryService 建立並回傳一個已完成依賴注入的 EquityHistoryService。
func NewEquityHistoryService(equity equityHistoryReader, log *logrus.Logger) *EquityHistoryService {
	return &EquityHistoryService{equity: equity, log: log}
}

// EquityHistory 回傳升冪的每日權益曲線 (等距取樣後);無資料時回傳空切片。
// 讀取失敗時回傳錯誤 (不吞錯),由 controller 決定對外的寬鬆契約。
func (s *EquityHistoryService) EquityHistory(ctx context.Context) ([]dto.LiveEquityPoint, error) {
	snaps, err := s.equity.ListEquityAsc(ctx)
	if err != nil {
		return nil, fmt.Errorf("list equity history: %w", err)
	}
	return liveEquityCurveDTO(snaps), nil
}

// liveEquityCurveDTO 把每日權益快照等距取樣並映射成 LiveEquityPoint 切片 (金額兩位小數);
// 期末點 (最後一日) 必定入列。輸入為空時回傳空切片 (非 nil)。
func liveEquityCurveDTO(snaps []entity.EquitySnapshot) []dto.LiveEquityPoint {
	n := len(snaps)
	if n == 0 {
		return []dto.LiveEquityPoint{}
	}
	stride := sampleStride(n, maxEquityCurvePoints)
	pts := make([]dto.LiveEquityPoint, 0, n/stride+1)
	// 以等距步長取樣每個權益快照。
	for i := 0; i < n; i += stride {
		pts = append(pts, liveEquityPoint(snaps[i]))
	}
	// 補上最後一日,若未恰好落在取樣步長上。
	if last := n - 1; last%stride != 0 {
		pts = append(pts, liveEquityPoint(snaps[last]))
	}
	return pts
}

// liveEquityPoint 把單筆權益快照映射成對外 DTO (金額兩位小數)。
func liveEquityPoint(s entity.EquitySnapshot) dto.LiveEquityPoint {
	return dto.LiveEquityPoint{
		Date:         s.Date,
		Cash:         round2(s.Cash),
		HoldingValue: round2(s.HoldingValue),
		TotalEquity:  round2(s.TotalEquity),
	}
}
