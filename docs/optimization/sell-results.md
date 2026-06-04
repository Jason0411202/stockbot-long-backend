# 賣出端 regime 切換實驗 (在策略 C 之上;聚焦回撤)

- 基底=策略C:basis=peak, regime=ma_pos(200), bull_band=0.25, bull_cd=5, trail_min_gain=0.20
- 掃描:trail_bear × trail_bull × sell_thr_bear × sell_thr_bull;共 80 種
- **C baseline (賣出端不動)**: CAGR +22.8% | MaxDD -24.6% | Calmar 0.73 | 曝險 +77.9% | 真擇時 +52.4%
- 目前 config.yaml(低利用率): CAGR +11.9% | MaxDD -15.3% | Calmar 1.10 | 曝險 39.8%
- Buy&Hold: CAGR +24.7% | MaxDD -32.8%

## 全情境 (依回撤由小到大;* = Pareto 報酬↑/回撤↓)
```
P CAGR   MaxDD  Calmar 曝險     真擇時    XIRR   賣出情境
* +14.2% -14.9% 1.00   +59.3% +52.4% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=0.3 thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +47.6% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=0.7 thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +47.6% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=0.7 thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +47.6% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=0.5 thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +47.6% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=1.0* thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +47.6% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=0.5 thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +52.4% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=0.3 thr_bull=1.0*
* +14.2% -14.9% 1.00   +59.3% +47.6% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=1.0* thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=0.5 thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=1.0* thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=0.7 thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=1.0* thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=0.3 thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=0.5 thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=0.3 thr_bull=1.0*
* +14.7% -17.4% 0.92   +60.4% +38.1% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=0.7 thr_bull=1.0*
  +14.2% -17.6% 0.85   +59.3% +33.3% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=1.0* thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +33.3% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=1.0* thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +33.3% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=0.7 thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +38.1% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=0.3 thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +33.3% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=0.5 thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +33.3% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=0.5 thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +38.1% +24.8% trail_bear=0.06 trail_bull=0.25 thr_bear=0.3 thr_bull=1.5
  +14.2% -17.6% 0.85   +59.3% +33.3% +24.8% trail_bear=0.06 trail_bull=0.00 thr_bear=0.7 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=0.7 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=0.3 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=0.7 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=0.5 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.25 thr_bear=1.0* thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=0.5 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=0.3 thr_bull=1.5
  +14.7% -17.7% 0.88   +60.4% +28.6% +23.9% trail_bear=0.10 trail_bull=0.00 thr_bear=1.0* thr_bull=1.5
  +11.0% -17.8% 0.71   +64.7% +38.1% +16.0% trail_bear=0.15 trail_bull=0.25 thr_bear=0.5 thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +33.3% +16.0% trail_bear=0.15 trail_bull=0.00 thr_bear=0.3 thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +38.1% +16.0% trail_bear=0.15 trail_bull=0.00 thr_bear=1.0* thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +38.1% +16.0% trail_bear=0.15 trail_bull=0.25 thr_bear=0.7 thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +33.3% +16.0% trail_bear=0.15 trail_bull=0.25 thr_bear=0.3 thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +38.1% +16.0% trail_bear=0.15 trail_bull=0.00 thr_bear=0.5 thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +38.1% +16.0% trail_bear=0.15 trail_bull=0.00 thr_bear=0.7 thr_bull=1.0*
  +11.0% -17.8% 0.71   +64.7% +38.1% +16.0% trail_bear=0.15 trail_bull=0.25 thr_bear=1.0* thr_bull=1.0*
  +11.2% -21.3% 0.71   +64.7% +28.6% +16.2% trail_bear=0.15 trail_bull=0.00 thr_bear=0.7 thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +28.6% +16.2% trail_bear=0.15 trail_bull=0.25 thr_bear=1.0* thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +23.8% +16.2% trail_bear=0.15 trail_bull=0.00 thr_bear=0.3 thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +28.6% +16.2% trail_bear=0.15 trail_bull=0.00 thr_bear=0.5 thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +28.6% +16.2% trail_bear=0.15 trail_bull=0.25 thr_bear=0.7 thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +23.8% +16.2% trail_bear=0.15 trail_bull=0.25 thr_bear=0.3 thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +28.6% +16.2% trail_bear=0.15 trail_bull=0.00 thr_bear=1.0* thr_bull=1.5
  +11.2% -21.3% 0.71   +64.7% +28.6% +16.2% trail_bear=0.15 trail_bull=0.25 thr_bear=0.5 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=0.3 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=0.3 thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=0.7 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=1.0* thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=1.0* thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=0.5 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=0.3 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=1.0* thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=1.0* thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=0.7 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=0.7 thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +38.1% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=0.5 thr_bull=1.5
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=0.5 thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.00 thr_bear=0.3 thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=0.5 thr_bull=1.0*
  +14.0% -23.6% 0.70   +67.0% +33.3% +19.9% trail_bear=0.25 trail_bull=0.25 thr_bear=0.7 thr_bull=1.0*
* +22.8% -24.6% 0.73   +77.9% +52.4% +29.3% trail_bear=0.00 trail_bull=0.00 thr_bear=0.7 thr_bull=1.0*
  +21.7% -24.6% 0.75   +78.0% +47.6% +27.2% trail_bear=0.00 trail_bull=0.00 thr_bear=0.5 thr_bull=1.5
  +21.7% -24.6% 0.75   +78.0% +47.6% +27.2% trail_bear=0.00 trail_bull=0.00 thr_bear=1.0* thr_bull=1.5
* +22.8% -24.6% 0.73   +77.8% +52.4% +29.3% trail_bear=0.00 trail_bull=0.25 thr_bear=0.7 thr_bull=1.0*
* +22.8% -24.6% 0.73   +77.8% +52.4% +29.3% trail_bear=0.00 trail_bull=0.25 thr_bear=1.0* thr_bull=1.0*
  +21.7% -24.6% 0.75   +78.0% +47.6% +27.2% trail_bear=0.00 trail_bull=0.00 thr_bear=0.7 thr_bull=1.5
* +22.8% -24.6% 0.73   +77.8% +52.4% +29.3% trail_bear=0.00 trail_bull=0.25 thr_bear=0.3 thr_bull=1.0*
  +21.7% -24.6% 0.75   +77.9% +47.6% +27.2% trail_bear=0.00 trail_bull=0.25 thr_bear=1.0* thr_bull=1.5
  +21.7% -24.6% 0.75   +77.9% +47.6% +27.2% trail_bear=0.00 trail_bull=0.25 thr_bear=0.3 thr_bull=1.5
* +22.8% -24.6% 0.73   +77.9% +52.4% +29.3% trail_bear=0.00 trail_bull=0.00 thr_bear=0.5 thr_bull=1.0*
  +21.7% -24.6% 0.75   +77.9% +47.6% +27.2% trail_bear=0.00 trail_bull=0.25 thr_bear=0.5 thr_bull=1.5
  +21.7% -24.6% 0.75   +78.0% +47.6% +27.2% trail_bear=0.00 trail_bull=0.00 thr_bear=0.3 thr_bull=1.5
  +21.7% -24.6% 0.75   +77.9% +47.6% +27.2% trail_bear=0.00 trail_bull=0.25 thr_bear=0.7 thr_bull=1.5
* +22.8% -24.6% 0.73   +77.9% +52.4% +29.3% trail_bear=0.00 trail_bull=0.00 thr_bear=0.3 thr_bull=1.0*
* +22.8% -24.6% 0.73   +77.9% +52.4% +29.3% trail_bear=0.00 trail_bull=0.00 thr_bear=1.0* thr_bull=1.0*
* +22.8% -24.6% 0.73   +77.8% +52.4% +29.3% trail_bear=0.00 trail_bull=0.25 thr_bear=0.5 thr_bull=1.0*
```

## 具名最佳

### 最低回撤 (CAGR≥15% 中 |MaxDD| 最小)
- CAGR **+22.8%**（C +22.8% / B&H +24.7%）| MaxDD **-24.6%**（C -24.6%）| Calmar **0.73** | 曝險 +77.9% | 真擇時 +52.4%
- 賣出設定:`trail_bear=0.00 trail_bull=0.00 thr_bear=0.7 thr_bull=1.0*`

### 最佳 CP 值 (Calmar 最高)
- CAGR **+14.2%**（C +22.8% / B&H +24.7%）| MaxDD **-14.9%**（C -24.6%）| Calmar **1.00** | 曝險 +59.3% | 真擇時 +47.6%
- 賣出設定:`trail_bear=0.06 trail_bull=0.25 thr_bear=1.0* thr_bull=1.0*`

### 控回撤衝報酬 (|MaxDD|≤20% 中 CAGR 最高)
- CAGR **+14.7%**（C +22.8% / B&H +24.7%）| MaxDD **-17.4%**（C -24.6%）| Calmar **0.92** | 曝險 +60.4% | 真擇時 +38.1%
- 賣出設定:`trail_bear=0.10 trail_bull=0.00 thr_bear=0.5 thr_bull=1.0*`

