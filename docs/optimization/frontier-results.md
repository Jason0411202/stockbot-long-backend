# 效率前緣 — regime × 部署量 × 熊市停利 聯合大掃描 (徹底定案)

- 固定核心: basis=peak, regime=ma_pos(200), ratio=2.0, base=500, sell=10000, sell_rsi=75, trail_min_gain=0.20, trail_bull=0
- 掃描: ma[5 10 20] × mult[2 3 4] × bull_band[0 0.1 0.25] × bull_cd[0 5] × trail_bear[0 0.05 0.08 0.12 0.2] = **270** 組
- 參考: 現況 config.yaml CAGR +11.9% / MaxDD -15.3% | B&H CAGR +24.7% / MaxDD -32.8%

## 效率前緣圖 (橫=最大回撤|MaxDD|, 縱=年化報酬CAGR;# 前緣, C 現況, B = B&H)
```
  28% |                          #                     
  26% |                                          B     
  24% |                     #    #                     
  22% |                    #                           
  21% |                   #                            
  19% |                   #                            
  17% |                 #                              
  15% |               #                                
  13% |             #     C                            
  11% |             #                                  
   9% |            #                                   
   7% |                                                
   6% |                                                
   4% |                                                
   2% |                                                
   0% |                                                
       +------------------------------------------------
        0                    18%                   36% |MaxDD|
```

## 效率前緣 (依回撤由小到大) — 每個風險水位能拿到的最高報酬
```
CAGR   MaxDD  Calmar 利用率    真擇時    XIRR   配置
+8.0%  -10.0% 0.94   +26.0% +38.1% +27.5% ma=20 mult=2 bull_band=0.00 bull_cd=0 trail_bear=0.08
+8.0%  -10.0% 0.97   +26.6% +52.4% +30.4% ma=20 mult=2 bull_band=0.00 bull_cd=0 trail_bear=0.12
+9.8%  -10.5% 0.88   +37.4% +42.9% +22.2% ma=5 mult=2 bull_band=0.00 bull_cd=0 trail_bear=0.08
+12.6% -10.6% 0.88   +35.1% +61.9% +30.1% ma=5 mult=2 bull_band=0.00 bull_cd=0 trail_bear=0.05
+13.3% -11.7% 1.10   +44.1% +52.4% +28.9% ma=5 mult=2 bull_band=0.10 bull_cd=0 trail_bear=0.08
+13.3% -11.7% 1.10   +44.1% +52.4% +28.9% ma=5 mult=2 bull_band=0.25 bull_cd=0 trail_bear=0.08
+16.7% -13.6% 0.98   +49.2% +66.7% +30.1% ma=5 mult=3 bull_band=0.00 bull_cd=0 trail_bear=0.05
+18.1% -14.7% 0.98   +54.4% +76.2% +29.1% ma=5 mult=3 bull_band=0.00 bull_cd=0 trail_bear=0.08
+20.0% -15.0% 1.12   +63.8% +71.4% +28.4% ma=5 mult=2 bull_band=0.10 bull_cd=5 trail_bear=0.08
+20.0% -15.0% 1.12   +63.8% +71.4% +28.4% ma=5 mult=2 bull_band=0.25 bull_cd=5 trail_bear=0.08
+21.7% -16.0% 1.15   +67.3% +76.2% +29.9% ma=5 mult=3 bull_band=0.00 bull_cd=5 trail_bear=0.08
+23.6% -16.6% 1.18   +73.8% +85.7% +30.7% ma=5 mult=3 bull_band=0.25 bull_cd=5 trail_bear=0.08
+23.6% -16.6% 1.18   +73.8% +85.7% +30.7% ma=5 mult=3 bull_band=0.10 bull_cd=5 trail_bear=0.08
+24.2% -20.0% 1.23   +75.1% +71.4% +31.5% ma=5 mult=4 bull_band=0.00 bull_cd=5 trail_bear=0.08
+26.3% -20.2% 1.31   +80.1% +81.0% +33.3% ma=5 mult=4 bull_band=0.10 bull_cd=5 trail_bear=0.08
+26.3% -20.2% 1.31   +80.1% +81.0% +33.3% ma=5 mult=4 bull_band=0.25 bull_cd=5 trail_bear=0.08
```

## 各回撤上限下的最佳配置

- **|MaxDD| ≤ 15%** → CAGR **+18.1%**, MaxDD -14.7%, Calmar 0.98, 利用率 +54.4%, 真擇時 +76.2%  〔`ma=5 mult=3 bull_band=0.00 bull_cd=0 trail_bear=0.08`〕
- **|MaxDD| ≤ 18%** → CAGR **+23.6%**, MaxDD -16.6%, Calmar 1.18, 利用率 +73.8%, 真擇時 +85.7%  〔`ma=5 mult=3 bull_band=0.10 bull_cd=5 trail_bear=0.08`〕
- **|MaxDD| ≤ 20%** → CAGR **+24.2%**, MaxDD -20.0%, Calmar 1.23, 利用率 +75.1%, 真擇時 +71.4%  〔`ma=5 mult=4 bull_band=0.00 bull_cd=5 trail_bear=0.08`〕
- **|MaxDD| ≤ 22%** → CAGR **+26.3%**, MaxDD -20.2%, Calmar 1.31, 利用率 +80.1%, 真擇時 +81.0%  〔`ma=5 mult=4 bull_band=0.10 bull_cd=5 trail_bear=0.08`〕
- **|MaxDD| ≤ 25%** → CAGR **+26.3%**, MaxDD -20.2%, Calmar 1.31, 利用率 +80.1%, 真擇時 +81.0%  〔`ma=5 mult=4 bull_band=0.10 bull_cd=5 trail_bear=0.08`〕
- **|MaxDD| ≤ 30%** → CAGR **+26.3%**, MaxDD -20.2%, Calmar 1.31, 利用率 +80.1%, 真擇時 +81.0%  〔`ma=5 mult=4 bull_band=0.10 bull_cd=5 trail_bear=0.08`〕

- **最高 Calmar (CP值)** → CAGR +26.3%, MaxDD -20.2%, Calmar **1.31**, 利用率 +80.1% 〔`ma=5 mult=4 bull_band=0.10 bull_cd=5 trail_bear=0.08`〕
