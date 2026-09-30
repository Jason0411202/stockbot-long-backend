package trading

import (
	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
	"time"
)

// SetSuspensions attaches official non-trading intervals. Completed actions
// without an announcement use the last observed bar to find the suspension.
func (s *StockSeries) SetSuspensions(id string, book marketunits.Book) {
	for _, a := range book.Actions {
		if a.StockID != id {
			continue
		}
		end, _ := time.Parse(time.DateOnly, a.Date)
		start, _ := time.Parse(time.DateOnly, a.SuspendFrom)
		if start.IsZero() {
			for i, d := range s.Dates {
				if !d.Before(end) && i > 0 && end.Sub(s.Dates[i-1]) <= 14*24*time.Hour {
					start = s.Dates[i-1].AddDate(0, 0, 1)
					break
				}
			}
		}
		if !start.IsZero() && start.Before(end) {
			s.Suspensions = append(s.Suspensions, [2]time.Time{start, end})
		}
	}
}
func (s *StockSeries) Suspended(day time.Time) bool {
	for _, interval := range s.Suspensions {
		if !day.Before(interval[0]) && day.Before(interval[1]) {
			return true
		}
	}
	return false
}
