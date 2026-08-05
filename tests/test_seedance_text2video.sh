#!/bin/bash
# Seedance 文生视频测试（tokease 环境，写死 BASE/KEY）
#
# 场景: 纯文本生成 9:16 竖屏广告视频（目标约 16 秒，模型上限 15s，取 15s），
#       同一提示词分别用 480p / 720p / 1080p 三档分辨率各生成一条。
#
# 与 test_seedance_e2e.sh 的区别:
#   - 不涉及素材库，仅文生视频
#   - BASE / TOKEN / 模型写死，直接运行即可: ./tests/test_seedance_text2video.sh

set -uo pipefail

BASE="https://tokease.cn"
TOKEN="sk-DCF15OpAVncYUHujtvQaM5wvl0G6XFwaCAD2UBw3SxtYaVRO"
MODEL="Seedance 2.0"
RATIO="9:16"
DURATION=15
RESOLUTIONS=(480p 720p 1080p)

PROMPT="竖屏果茶广告：开场特写，冰块坠入透明杯中溅起金黄果汁，慢镜头；镜头缓缓旋转上升，展示挂满水珠的瓶身，背景是明亮的夏日阳光和虚化的绿叶；新鲜橙子切片旋转飞入画面；结尾产品居中定格，光斑闪烁，留白收尾。明快清新色调，电影级质感，禁止出现任何文字和字幕。"

POLL_INTERVAL=12
VIDEO_TIMEOUT=900

green() { printf "\033[32m%s\033[0m" "$1"; }
red()   { printf "\033[31m%s\033[0m" "$1"; }
bold()  { printf "\033[1m%s\033[0m" "$1"; }
step()  { echo ""; echo "$(bold "▶ $1")"; }

# jget <python表达式>：从 stdin 的 JSON 中取值（d 为解析后的对象），失败输出空串
jget() {
    python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
    v = eval(sys.argv[1])
    print("" if v is None else v)
except Exception:
    print("")' "$1"
}

echo "$(bold "Seedance 文生视频测试（多分辨率广告）")"
echo "  BASE=$BASE"
echo "  MODEL=$MODEL  ratio=$RATIO  duration=${DURATION}s"
echo "  分辨率: ${RESOLUTIONS[*]}"

# ── 1. 逐分辨率提交任务 ───────────────────────────────────
TASK_IDS=()
for RES in "${RESOLUTIONS[@]}"; do
    step "提交 $RES"
    RESP=$(curl -sS -X POST "$BASE/v1/video/generations" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
        --max-time 120 -d "{
        \"model\": \"$MODEL\",
        \"prompt\": $(printf '%s' "$PROMPT" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),
        \"metadata\": {
            \"resolution\": \"$RES\",
            \"ratio\": \"$RATIO\",
            \"duration\": $DURATION
        }
    }")
    # 兼容两种提交响应：本网关 {task_id,...}；tokease 等只返回 {id,...}
    TID=$(printf '%s' "$RESP" | jget 'd.get("task_id") or d.get("id")')
    if [ -z "$TID" ]; then
        echo "  $(red FAIL) $RES 提交失败"
        echo "       $RESP"
        TASK_IDS+=("")
    else
        echo "  $(green OK) $RES → $TID"
        TASK_IDS+=("$TID")
    fi
done

# ── 2. 轮询全部任务 ──────────────────────────────────────
step "轮询任务（超时 ${VIDEO_TIMEOUT}s）"
FINAL_STATUS=()
FINAL_URLS=()
for RES in "${RESOLUTIONS[@]}"; do FINAL_STATUS+=("PENDING"); FINAL_URLS+=(""); done

DEADLINE=$(($(date +%s) + VIDEO_TIMEOUT))
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    ALL_DONE=1
    LINE=""
    for i in "${!RESOLUTIONS[@]}"; do
        TID="${TASK_IDS[$i]}"
        [ -z "$TID" ] && { FINAL_STATUS[$i]="SUBMIT_FAIL"; continue; }
        case "${FINAL_STATUS[$i]}" in SUCCESS|FAILURE|SUBMIT_FAIL|QUERY_ERROR) continue ;; esac
        RESP=$(curl -sS "$BASE/v1/video/generations/$TID" \
            -H "Authorization: Bearer $TOKEN" --max-time 60)
        # 兼容两种查询响应：本网关 {code,data:{status,...}}；OpenAI 风格顶层 {status,...}
        ST=$(printf '%s' "$RESP" | jget '(d.get("data") or {}).get("status") or d.get("status")')
        PG=$(printf '%s' "$RESP" | jget '(d.get("data") or {}).get("progress") or d.get("progress")')
        # 查询接口直接报错（如令牌模型白名单拒绝）时，暴露错误并终止该任务轮询
        ERR=$(printf '%s' "$RESP" | jget '(d.get("error") or {}).get("message")')
        if [ -z "$ST" ] && [ -n "$ERR" ]; then
            FINAL_STATUS[$i]="QUERY_ERROR"
            FINAL_URLS[$i]="$ERR"
            continue
        fi
        LINE="$LINE ${RESOLUTIONS[$i]}=${ST:-?}(${PG:-?})"
        case "$ST" in
        SUCCESS|succeeded|completed)
            FINAL_STATUS[$i]="SUCCESS"
            FINAL_URLS[$i]=$(printf '%s' "$RESP" | jget '((d.get("data") or {}).get("data") or {}).get("content",{}).get("video_url") or (d.get("data") or {}).get("video_url") or d.get("video_url")')
            ;;
        FAILURE|failed)
            FINAL_STATUS[$i]="FAILURE"
            FINAL_URLS[$i]=$(printf '%s' "$RESP" | jget '(d.get("data") or {}).get("fail_reason") or d.get("fail_reason")')
            ;;
        *)
            ALL_DONE=0
            ;;
        esac
    done
    [ -n "$LINE" ] && echo "   $LINE"
    [ "$ALL_DONE" = "1" ] && break
    sleep "$POLL_INTERVAL"
done

# ── 3. 结果汇总 ──────────────────────────────────────────
echo ""
echo "$(bold "结果汇总")"
EXIT=0
for i in "${!RESOLUTIONS[@]}"; do
    RES="${RESOLUTIONS[$i]}"
    ST="${FINAL_STATUS[$i]}"
    if [ "$ST" = "SUCCESS" ]; then
        echo "  $(green "✓ $RES") 任务=${TASK_IDS[$i]}"
        echo "      ${FINAL_URLS[$i]}"
    else
        echo "  $(red "✗ $RES") 状态=$ST 任务=${TASK_IDS[$i]:-无}"
        [ -n "${FINAL_URLS[$i]}" ] && echo "      ${FINAL_URLS[$i]}"
        EXIT=1
    fi
done
exit $EXIT
