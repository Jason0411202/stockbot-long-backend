// Package marketunits keeps paper-account units independent of corporate actions.
// Market prices are converted once at the boundary. The ledger, risk anchors and
// integer sizing all keep the same unit for their entire lifetime.
package marketunits

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// BasisDate is a versioned accounting convention, NEVER the last loaded bar.
// Moving it on a refresh/restart would change holdings, rounding and past trades.
const BasisDate = "2026-09-30"

type Action struct {
	StockID     string  `json:"stock_id"`
	Date        string  `json:"date"` // first session in the new unit
	SuspendFrom string  `json:"suspend_from,omitempty"`
	Ratio       float64 `json:"ratio"` // new shares / old shares
}

type Book struct {
	Basis   string   `json:"basis"`
	Actions []Action `json:"actions"`
}

// Default includes the official action affecting the existing account. Offline
// research uses the same basis; online announcements extend this persisted book.
func Default() Book {
	return Book{Basis: BasisDate, Actions: []Action{{StockID: "00631L", Date: "2026-03-31", SuspendFrom: "2026-03-25", Ratio: 22}}}
}

func (b Book) Validate() error {
	if b.Basis != BasisDate {
		return fmt.Errorf("unsupported accounting unit basis %q", b.Basis)
	}
	seen := map[string]bool{}
	for _, a := range b.Actions {
		if _, err := time.Parse("2006-01-02", a.Date); err != nil {
			return err
		}
		if a.SuspendFrom != "" {
			if _, err := time.Parse("2006-01-02", a.SuspendFrom); err != nil {
				return err
			}
			if a.SuspendFrom >= a.Date {
				return fmt.Errorf("invalid suspension for %s", a.StockID)
			}
		}
		key := a.StockID + "/" + a.Date
		if a.StockID == "" || a.Ratio <= 0 || a.Ratio == 1 || math.IsNaN(a.Ratio) || math.IsInf(a.Ratio, 0) || seen[key] {
			return fmt.Errorf("invalid/duplicate action %s", key)
		}
		seen[key] = true
	}
	return nil
}

func (b Book) Merge(actions []Action) (Book, error) {
	out := Book{Basis: b.Basis, Actions: append([]Action(nil), b.Actions...)}
	for _, a := range actions {
		found := false
		for i, old := range out.Actions {
			if old.StockID != a.StockID || old.Date != a.Date {
				continue
			}
			if math.Abs(old.Ratio-a.Ratio) > 1e-7*old.Ratio {
				return Book{}, fmt.Errorf("conflicting official action %s %s", a.StockID, a.Date)
			}
			if a.SuspendFrom != "" {
				out.Actions[i].SuspendFrom = a.SuspendFrom
			}
			found = true
			break
		}
		if !found {
			out.Actions = append(out.Actions, a)
		}
	}
	sort.Slice(out.Actions, func(i, j int) bool {
		return out.Actions[i].StockID+out.Actions[i].Date < out.Actions[j].StockID+out.Actions[j].Date
	})
	return out, out.Validate()
}

// Factor converts a raw exchange quote to the permanent accounting unit.
// Before the basis: back-adjust; after the basis: forward-adjust. This also
// preserves reverse-split fractions without rounding away economic ownership.
func (b Book) Factor(id, date string) float64 {
	factor := 1.0
	for _, a := range b.Actions {
		if a.StockID != id {
			continue
		}
		if a.Date <= date && a.Date > b.Basis {
			factor *= a.Ratio
		}
		if a.Date > date && a.Date <= b.Basis {
			factor /= a.Ratio
		}
	}
	return factor
}

// DisplayFactor rebases every displayed price AND quantity to today's unit.
// Monetary cost, proceeds, P&L and equity never need changing.
func (b Book) DisplayFactor(id, today string) float64 { return b.Factor(id, today) }

func (b Book) Suspended(id, date string) bool {
	for _, a := range b.Actions {
		if a.StockID == id && a.SuspendFrom != "" && date >= a.SuspendFrom && date < a.Date {
			return true
		}
	}
	return false
}

// Normalize handles OHLC together and volume inversely; it never infers an
// action from actual trading returns or uses a later close as a split ratio.
func (b Book) Normalize(id string, dates []time.Time, prices [][]float64, volumes []float64) {
	for i, d := range dates {
		f := b.Factor(id, d.Format("2006-01-02"))
		for _, p := range prices {
			if i < len(p) {
				p[i] *= f
			}
		}
		if i < len(volumes) {
			volumes[i] /= f
		}
	}
}
