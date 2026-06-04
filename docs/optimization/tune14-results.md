# 冷卻固定 = 14 天 (2 周) 下的最佳參數 (net-of-fee)

- 固定: cooldown=14 (bull=bear), basis=peak, regime=ma_pos(200), ratio=2, base=500, mult=3, trail_min_gain=0.20, sell_rsi=75
- 掃描: ma_window × bull_buy_amount × bull_buy_band × trail_stop_bear;共 252 組;計入手續費+稅
- 對照: 現況 config.yaml(低利用率) ≈ CAGR +11.9% / MaxDD -15.3% / Calmar 1.10 / 曝險 40%

## 效率前緣 (冷卻=14 約束內;依回撤由小到大)
```
CAGR   MaxDD  Calmar 曝險     交易/窗 配置
+15.0% -15.3% 0.93   +49.6% 141  ma=20 bull買=1500 band=0.05 trail=0.08
+15.1% -15.7% 0.88   +52.5% 149  ma=5 bull買=1500 band=0.25 trail=0.05
+15.1% -15.7% 0.88   +52.5% 149  ma=5 bull買=1500 band=0.10 trail=0.05
+17.5% -16.0% 0.88   +55.8% 150  ma=5 bull買=1500 band=0.10 trail=0.08
+17.5% -16.0% 0.88   +55.8% 150  ma=5 bull買=1500 band=0.25 trail=0.08
+22.3% -18.5% 1.04   +71.2% 98   ma=10 bull買=3000 band=0.05 trail=0.08
+22.7% -20.7% 1.03   +74.1% 92   ma=20 bull買=4500 band=0.05 trail=0.08
+23.1% -21.4% 1.24   +78.3% 87   ma=5 bull買=4500 band=0.25 trail=0.08
+23.1% -21.4% 1.24   +78.3% 87   ma=5 bull買=4500 band=0.10 trail=0.08
+23.1% -21.4% 1.24   +78.6% 87   ma=5 bull買=4500 band=0.05 trail=0.08
+23.6% -22.4% 1.12   +78.0% 81   ma=10 bull買=4500 band=0.10 trail=0.08
+23.6% -22.4% 1.12   +78.0% 81   ma=10 bull買=4500 band=0.25 trail=0.08
+23.6% -22.4% 1.05   +76.2% 87   ma=20 bull買=4500 band=0.10 trail=0.08
+23.7% -22.4% 1.14   +77.9% 81   ma=10 bull買=4500 band=0.05 trail=0.08
+26.0% -22.6% 1.24   +80.9% 66   ma=10 bull買=6000 band=0.25 trail=0.08
+26.0% -22.6% 1.24   +80.9% 66   ma=10 bull買=6000 band=0.10 trail=0.08
+26.1% -22.7% 1.25   +80.9% 66   ma=10 bull買=6000 band=0.05 trail=0.08
```

## 具名最佳 (冷卻=14)

### ★ 風險調整最強 (Calmar 最高)
- CAGR **+26.1%** | MaxDD **-22.7%** | Calmar **1.25** | 資金利用率 **+80.9%** | 交易/窗 **66**
- 配置:`ma=10 bull買=6000 band=0.05 trail=0.08`

### 低回撤 (|MaxDD|≤18% 取 CAGR 最高)
- CAGR **+17.5%** | MaxDD **-16.0%** | Calmar **0.88** | 資金利用率 **+55.8%** | 交易/窗 **150**
- 配置:`ma=5 bull買=1500 band=0.10 trail=0.08`

### 高利用率 (|MaxDD|≤22% 取 CAGR 最高)
- CAGR **+23.1%** | MaxDD **-21.4%** | Calmar **1.24** | 資金利用率 **+78.6%** | 交易/窗 **87**
- 配置:`ma=5 bull買=4500 band=0.05 trail=0.08`

## 建議寫入 config.yaml (Calmar 首選)
```yaml
ma_window: 10
buy_depth_basis: peak
regime_method: ma_pos
regime_ma_window: 200
bull_buy_band: 0.05
bull_buy_amount: 2000      # ×multiplier(3) → 多頭每次實買 6000
bull_cooldown_days: 14
cooldown_days: 14          # bear 冷卻 (=2 周)
buy_tier_ratio: 2.0
buy_base_amount: 500
trail_stop_bear: 0.08
trail_min_gain: 0.20
# 已在 config:multiplier:3, sell_rsi 14/75, fee_rate, sell_tax_rate
```
