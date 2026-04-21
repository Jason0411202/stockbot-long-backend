#!/usr/bin/env bash
# Plan v1 — parallel runner: baseline (Pyramid) vs new (Pyramid + TF branch)
#
# Usage:
#   bash scripts/run_plan_v1.sh {baseline|new|both} [out_root]
#
# Environment overrides:
#   SEED            default 0 (保留接口;回測本身 deterministic)
#   EXTRA_ARGS      e.g. "--back_testing_days_override=60"
#                   用於縮短一次回測的「工作量」(等同 max_steps 這類通用 debug 旋鈕)。
#   TF_TAU          default 0.02   (多頭判定閾值)
#   TF_AMOUNT_MODE  default const  ("const" or "cashfrac")
#   TF_ALPHA        default 2.0    (const 模式倍率)
#   TF_BETA         default 0.02   (cashfrac 模式比例)
#
# 並行語意:baseline 與 new 為兩個獨立 Go process,兩者只讀 DB、各自寫
# 不同 output_dir,彼此不共享可變資源。並行達成在本專案(CPU-only)的等效
# 意義——等同於 "GPU=0 vs GPU=1" 分別獨立跑。
set -euo pipefail

MODE="${1:-both}"
OUT_ROOT="${2:-runs/plan_v1_$(date +%Y%m%d_%H%M%S)}"
SEED="${SEED:-0}"
EXTRA_ARGS="${EXTRA_ARGS:-}"

TF_TAU="${TF_TAU:-0.02}"
TF_AMOUNT_MODE="${TF_AMOUNT_MODE:-const}"
TF_ALPHA="${TF_ALPHA:-2.0}"
TF_BETA="${TF_BETA:-0.02}"

mkdir -p "$OUT_ROOT/baseline" "$OUT_ROOT/new"

run_one() {
  local JOB_TYPE="$1"
  local -a FLAGS
  # shellcheck disable=SC2206
  FLAGS=($2)
  go run ./cmd/research_run \
    --seed="$SEED" \
    --output_dir="$OUT_ROOT/$JOB_TYPE" \
    "${FLAGS[@]}" \
    $EXTRA_ARGS \
    2>&1 | tee "$OUT_ROOT/$JOB_TYPE.log"
}

BASELINE_FLAGS="--use_tf=false"
NEW_FLAGS="--use_tf=true --tf_tau=$TF_TAU --tf_amount_mode=$TF_AMOUNT_MODE --tf_alpha=$TF_ALPHA --tf_beta=$TF_BETA"

case "$MODE" in
  baseline)
    run_one baseline "$BASELINE_FLAGS"
    ;;
  new)
    run_one new "$NEW_FLAGS"
    ;;
  both)
    run_one baseline "$BASELINE_FLAGS" & PID_B=$!
    run_one new      "$NEW_FLAGS"      & PID_N=$!
    wait "$PID_B" "$PID_N"
    ;;
  *)
    echo "usage: $0 {baseline|new|both} [out_root]" >&2
    exit 2
    ;;
esac

echo "=== Plan v1 done -> $OUT_ROOT ==="
