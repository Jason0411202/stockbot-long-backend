# 牛熊 regime + 加碼深度基準 實驗

- 部署量沿用 config.yaml 現值 (ma=20, ratio=2.0, base=500, mult=2, sell=10000, cd=14, sell_rsi=75)
- 只變動:加碼深度基準 × 牛熊判定法 × 牛市進場放寬(band)/冷卻;共 57 種情境
- 目前 config.yaml: CAGR +11.9% | MaxDD -15.3% | Calmar 1.10 | 曝險 39.8% | 真擇時 57%
- Buy&Hold: CAGR +24.7% | MaxDD -32.8%

## 全情境 (依資金利用率升冪;* = Pareto 前緣)
```
P CAGR   MaxDD  Calmar 曝險     真擇時    XIRR   情境
* +8.9%  -10.4% 1.03   +31.1% +57.1% +31.8% basis=ma        regime=off      band=0.00 bullcd=0
  +11.9% -15.3% 1.10   +39.8% +57.1% +33.2% basis=held_high regime=off      band=0.00 bullcd=0
* +10.3% -14.0% 0.86   +45.2% +47.6% +29.2% basis=ma        regime=ma_slope band=0.05 bullcd=0
  +10.4% -14.8% 0.80   +45.9% +47.6% +28.1% basis=ma        regime=ma_slope band=0.25 bullcd=0
  +10.4% -14.8% 0.80   +45.9% +47.6% +28.1% basis=ma        regime=ma_slope band=0.10 bullcd=0
  +11.9% -14.3% 0.88   +45.9% +47.6% +28.8% basis=ma        regime=mom      band=0.05 bullcd=0
* +12.5% -14.1% 0.89   +46.9% +52.4% +28.8% basis=ma        regime=ma_pos   band=0.05 bullcd=0
* +12.5% -15.1% 0.85   +47.6% +47.6% +28.0% basis=ma        regime=mom      band=0.10 bullcd=0
* +12.5% -15.1% 0.84   +47.6% +47.6% +27.8% basis=ma        regime=mom      band=0.25 bullcd=0
* +12.7% -15.2% 0.83   +48.8% +52.4% +27.6% basis=ma        regime=ma_pos   band=0.25 bullcd=0
* +12.7% -15.2% 0.83   +48.8% +52.4% +27.6% basis=ma        regime=ma_pos   band=0.10 bullcd=0
* +12.3% -19.0% 1.05   +49.5% +61.9% +31.3% basis=peak      regime=off      band=0.00 bullcd=0
* +12.5% -20.8% 0.87   +55.3% +52.4% +27.4% basis=held_high regime=ma_pos   band=0.05 bullcd=0
* +12.1% -20.8% 0.85   +55.5% +52.4% +27.4% basis=held_high regime=mom      band=0.05 bullcd=0
* +11.9% -21.0% 0.83   +55.8% +52.4% +27.3% basis=held_high regime=ma_slope band=0.05 bullcd=0
  +12.6% -23.2% 0.80   +57.2% +52.4% +26.0% basis=held_high regime=ma_pos   band=0.10 bullcd=0
  +12.6% -23.2% 0.80   +57.2% +52.4% +26.0% basis=held_high regime=ma_pos   band=0.25 bullcd=0
  +12.3% -23.2% 0.81   +57.4% +52.4% +25.6% basis=held_high regime=mom      band=0.10 bullcd=0
  +12.2% -23.2% 0.81   +57.4% +52.4% +26.0% basis=held_high regime=mom      band=0.25 bullcd=0
  +12.1% -22.6% 0.77   +57.8% +52.4% +26.0% basis=held_high regime=ma_slope band=0.10 bullcd=0
  +12.1% -22.6% 0.77   +57.8% +52.4% +26.0% basis=held_high regime=ma_slope band=0.25 bullcd=0
  +15.4% -22.8% 0.82   +60.2% +52.4% +27.6% basis=peak      regime=ma_pos   band=0.05 bullcd=0
  +11.7% -22.4% 0.82   +60.9% +52.4% +26.2% basis=peak      regime=ma_slope band=0.05 bullcd=0
  +11.9% -22.4% 0.81   +61.2% +52.4% +25.9% basis=peak      regime=ma_slope band=0.25 bullcd=0
  +15.7% -23.2% 0.81   +61.2% +52.4% +27.0% basis=peak      regime=ma_pos   band=0.25 bullcd=0
  +14.7% -22.9% 0.82   +61.3% +52.4% +26.5% basis=peak      regime=mom      band=0.05 bullcd=0
  +11.9% -22.4% 0.81   +61.4% +52.4% +25.9% basis=peak      regime=ma_slope band=0.10 bullcd=0
  +15.7% -23.2% 0.81   +61.8% +52.4% +27.0% basis=peak      regime=ma_pos   band=0.10 bullcd=0
  +14.9% -23.3% 0.81   +62.4% +52.4% +26.2% basis=peak      regime=mom      band=0.25 bullcd=0
  +14.9% -23.3% 0.81   +62.4% +52.4% +26.3% basis=peak      regime=mom      band=0.10 bullcd=0
  +13.8% -23.5% 0.73   +67.5% +33.3% +24.5% basis=ma        regime=ma_slope band=0.05 bullcd=5
  +13.9% -23.4% 0.73   +69.0% +33.3% +24.6% basis=ma        regime=ma_slope band=0.10 bullcd=5
  +13.9% -23.4% 0.73   +69.0% +33.3% +24.6% basis=ma        regime=ma_slope band=0.25 bullcd=5
* +16.6% -21.9% 0.75   +71.2% +42.9% +24.9% basis=ma        regime=ma_pos   band=0.05 bullcd=5
  +15.8% -26.5% 0.67   +71.5% +33.3% +24.8% basis=held_high regime=ma_slope band=0.05 bullcd=5
  +17.5% -24.6% 0.74   +71.6% +38.1% +25.7% basis=ma        regime=mom      band=0.05 bullcd=5
  +17.5% -22.7% 0.74   +72.3% +47.6% +25.0% basis=ma        regime=ma_pos   band=0.10 bullcd=5
* +18.5% -22.7% 0.74   +72.3% +47.6% +28.6% basis=ma        regime=ma_pos   band=0.25 bullcd=5
  +16.1% -26.6% 0.68   +72.8% +33.3% +24.9% basis=held_high regime=ma_slope band=0.25 bullcd=5
  +16.1% -26.6% 0.68   +72.8% +33.3% +24.9% basis=held_high regime=ma_slope band=0.10 bullcd=5
  +17.9% -24.5% 0.73   +72.9% +38.1% +25.7% basis=ma        regime=mom      band=0.10 bullcd=5
* +17.9% -24.5% 0.73   +72.9% +38.1% +25.7% basis=ma        regime=mom      band=0.25 bullcd=5
* +18.0% -24.6% 0.68   +75.9% +42.9% +25.2% basis=held_high regime=ma_pos   band=0.05 bullcd=5
  +18.5% -25.0% 0.67   +76.1% +33.3% +26.0% basis=held_high regime=mom      band=0.05 bullcd=5
  +21.6% -24.7% 0.73   +77.0% +52.4% +29.7% basis=peak      regime=ma_pos   band=0.05 bullcd=5
  +17.3% -27.3% 0.73   +77.1% +47.6% +24.7% basis=peak      regime=ma_slope band=0.05 bullcd=5
  +18.7% -24.6% 0.68   +77.2% +47.6% +25.3% basis=held_high regime=ma_pos   band=0.10 bullcd=5
  +19.3% -24.6% 0.68   +77.2% +47.6% +28.9% basis=held_high regime=ma_pos   band=0.25 bullcd=5
  +19.0% -25.1% 0.68   +77.3% +33.3% +26.1% basis=held_high regime=mom      band=0.10 bullcd=5
  +19.0% -25.1% 0.68   +77.3% +33.3% +26.1% basis=held_high regime=mom      band=0.25 bullcd=5
  +21.3% -26.2% 0.73   +77.3% +52.4% +28.1% basis=peak      regime=mom      band=0.05 bullcd=5
  +17.5% -27.3% 0.74   +77.7% +47.6% +24.7% basis=peak      regime=ma_slope band=0.10 bullcd=5
  +17.5% -27.3% 0.74   +77.7% +47.6% +24.7% basis=peak      regime=ma_slope band=0.25 bullcd=5
* +22.8% -24.6% 0.73   +77.9% +52.4% +29.3% basis=peak      regime=ma_pos   band=0.25 bullcd=5
  +22.2% -24.6% 0.73   +77.9% +52.4% +29.3% basis=peak      regime=ma_pos   band=0.10 bullcd=5
  +21.4% -26.2% 0.74   +77.9% +52.4% +28.1% basis=peak      regime=mom      band=0.10 bullcd=5
  +21.4% -26.2% 0.74   +77.9% +52.4% +28.1% basis=peak      regime=mom      band=0.25 bullcd=5
```

## 實驗A:加碼深度基準 (regime 關閉,其餘同現況)
```
深度基準      CAGR   MaxDD  Calmar 曝險     真擇時
held_high +11.9% -15.3% 1.10   +39.8% +57.1%
ma        +8.9%  -10.4% 1.03   +31.1% +57.1%
peak      +12.3% -19.0% 1.05   +49.5% +61.9%
```

## 具名最佳

### 最高 Calmar (CP值)
- CAGR **+11.9%**（B&H +24.7%）| MaxDD **-15.3%** | Calmar **1.10** | 資金利用率 **+39.8%** | 真擇時 +57.1%
- 情境:`basis=held_high regime=off      band=0.00 bullcd=0`

### 高利用率仍保 CP≥1.0 (曝險≥55% 且 Calmar≥1.0 且 |MDD|≤25%, 取曝險最高)
（無符合條件）

### 控風險衝報酬 (|MDD|≤22% 取 CAGR 最高)
- CAGR **+16.6%**（B&H +24.7%）| MaxDD **-21.9%** | Calmar **0.75** | 資金利用率 **+71.2%** | 真擇時 +42.9%
- 情境:`basis=ma        regime=ma_pos   band=0.05 bullcd=5`

