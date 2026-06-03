package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write tmp config: %v", err)
	}
	return p
}

const baseYaml = `
track_stocks:
  - "006208"
scaling_strategy: Baseline
buy_and_sell_multiplier: 2.0
max_back_months: 1
cooldown_days: 14
baseline_buy_tiers:
  - { above: -0.1, amount: 500 }
baseline_buy_fallback_amount: 3000
baseline_sell_threshold: 1.0
baseline_sell_amount: 10000
initial_cash: 1000000
`

func TestLoad_BackTestingMonthsSanityCheck(t *testing.T) {
	cases := []struct {
		name              string
		initDBBackMonths  int
		backTestingMonths int
		wantErr           bool
		wantErrContains   string
	}{
		{
			name:              "disabled backtest passes regardless of init months",
			initDBBackMonths:  1,
			backTestingMonths: -1,
			wantErr:           false,
		},
		{
			name:              "zero backtest passes regardless of init months",
			initDBBackMonths:  1,
			backTestingMonths: 0,
			wantErr:           false,
		},
		{
			name:              "exact equality passes (60 == 60)",
			initDBBackMonths:  60,
			backTestingMonths: 60,
			wantErr:           false,
		},
		{
			name:              "just under passes (59 < 60)",
			initDBBackMonths:  60,
			backTestingMonths: 59,
			wantErr:           false,
		},
		{
			name:              "just over rejects (61 > 60)",
			initDBBackMonths:  60,
			backTestingMonths: 61,
			wantErr:           true,
			wantErrContains:   "back_testing_months=61",
		},
		{
			name:              "the original README footgun: months mismatch by ~3x",
			initDBBackMonths:  60,
			backTestingMonths: 180,
			wantErr:           true,
			wantErrContains:   "init_db_back_months=60",
		},
		{
			name:              "180 init supports 180 backtest",
			initDBBackMonths:  180,
			backTestingMonths: 180,
			wantErr:           false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := baseYaml +
				"init_db_back_months: " + itoa(tc.initDBBackMonths) + "\n" +
				"back_testing_months: " + itoa(tc.backTestingMonths) + "\n"
			p := writeConfig(t, body)

			_, err := Load(p)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoad_TrackStocksRequired(t *testing.T) {
	body := `
scaling_strategy: Baseline
max_back_months: 1
init_db_back_months: 60
back_testing_months: -1
baseline_buy_fallback_amount: 3000
baseline_sell_threshold: 1.0
baseline_sell_amount: 10000
initial_cash: 1000000
`
	p := writeConfig(t, body)
	if _, err := Load(p); err == nil {
		t.Fatalf("expected error for empty track_stocks")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
