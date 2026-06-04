# 穩健路線 — 『買進冷卻 ≥ 14 天』約束下的最佳參數 (net-of-fee)

- 固定核心: basis=peak, regime=ma_pos(200), ratio=2, base=500, mult=3, bull_band=0.10, trail_min_gain=0.20, sell_rsi=75
- 約束: bull_cd ≥ 14 且 bear_cd ≥ 14;成本計入 (手續費 0.1425%/邊 + 稅 0.10%/賣);共 120 組
- 對照: 現況 config.yaml(低利用率) net ≈ CAGR +11.9% / MaxDD -15.3% / Calmar 1.10 / 曝險 40%

## 效率前緣 (約束內;依回撤由小到大)
```
CAGR   MaxDD  Calmar 曝險     交易/窗 配置
+19.9% -15.7% 1.15   +69.1% 87   ma=10 bull買=4500 bull冷卻=21 bear冷卻=21 trail=0.08
+22.1% -16.6% 1.27   +73.3% 71   ma=10 bull買=6000 bull冷卻=21 bear冷卻=21 trail=0.08
+23.0% -18.3% 1.29   +77.0% 75   ma=5 bull買=6000 bull冷卻=21 bear冷卻=14 trail=0.08
+24.6% -18.8% 1.37   +78.8% 62   ma=5 bull買=9000 bull冷卻=28 bear冷卻=14 trail=0.08
+25.4% -19.8% 1.28   +81.8% 60   ma=5 bull買=9000 bull冷卻=21 bear冷卻=14 trail=0.08
+26.1% -21.5% 1.28   +81.3% 56   ma=10 bull買=9000 bull冷卻=21 bear冷卻=14 trail=0.08
+26.3% -22.3% 1.39   +85.3% 38   ma=10 bull買=18000 bull冷卻=21 bear冷卻=21 trail=0.08
```

## 具名最佳 (約束內)

### ★ 穩健首選 (Calmar 最高)
- CAGR **+24.4%** | MaxDD **-20.8%** | Calmar **1.40** | 資金利用率 **+84.0%** | 交易/窗 **43**
- 配置:`ma=10 bull買=13500 bull冷卻=21 bear冷卻=21 trail=0.08`

### 低回撤 (|MaxDD|≤18% 取 CAGR 最高)
- CAGR **+22.1%** | MaxDD **-16.6%** | Calmar **1.27** | 資金利用率 **+73.3%** | 交易/窗 **71**
- 配置:`ma=10 bull買=6000 bull冷卻=21 bear冷卻=21 trail=0.08`

### 高利用率 (|MaxDD|≤22% 取 CAGR 最高)
- CAGR **+26.1%** | MaxDD **-21.5%** | Calmar **1.28** | 資金利用率 **+81.3%** | 交易/窗 **56**
- 配置:`ma=10 bull買=9000 bull冷卻=21 bear冷卻=14 trail=0.08`

## 建議寫入 config.yaml 的旋鈕 (穩健首選)
```yaml
ma_window: 10
buy_tier_ratio: 2.0
buy_base_amount: 500
buy_depth_basis: peak
regime_method: ma_pos
regime_ma_window: 200
bull_buy_band: 0.10
bull_buy_amount: 4500      # 多頭固定大額 (×multiplier=3 → 實際 13500)
bull_cooldown_days: 21
cooldown_days: 21          # bear 冷卻
trail_stop_bear: 0.08
trail_min_gain: 0.20
# (sell_rsi_period:14 / sell_rsi_min:75 / multiplier:3 已在 config)
```
