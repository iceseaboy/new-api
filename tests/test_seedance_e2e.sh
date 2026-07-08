#!/bin/bash
# Seedance 端到端测试：素材组 → 素材（审核通过）→ 基于图片素材生成视频
#
# 流程:
#   1. CreateAssetGroup                 → 拿 GroupId
#   2. CreateAsset (Image)              → 拿 AssetId
#   3. GetAsset 轮询至 Status=Active    （审核通过）
#   4. POST /v1/video/generations       （content: text + image_url=asset://<id> first_frame）
#   5. GET  /v1/video/generations/{id}  轮询至 SUCCESS，打印视频 URL
#
# 用法:
#   TOKEN=sk-xxx ./tests/test_seedance_e2e.sh
#   TOKEN=sk-xxx BASE=http://127.0.0.1:3000 ./tests/test_seedance_e2e.sh
#
# 环境变量覆盖:
#   BASE / VIDEO_MODEL / IMAGE_URL / RESOLUTION / DURATION / ASSET_TIMEOUT / VIDEO_TIMEOUT

set -uo pipefail

TOKEN="${TOKEN:-}"
BASE="${BASE:-https://www.aigclink.cc}"
VIDEO_MODEL="${VIDEO_MODEL:-doubao-seedance-2-0-260128}"
IMAGE_URL="${IMAGE_URL:-https://free.picui.cn/free/2026/04/11/69da1b9003787.png}"
RESOLUTION="${RESOLUTION:-480p}"
DURATION="${DURATION:-5}"
ASSET_POLL_INTERVAL=3
ASSET_TIMEOUT="${ASSET_TIMEOUT:-180}"
VIDEO_POLL_INTERVAL=10
VIDEO_TIMEOUT="${VIDEO_TIMEOUT:-600}"

if [ -z "$TOKEN" ]; then
    echo "用法: TOKEN=sk-xxx $0"
    echo "可选: BASE=$BASE VIDEO_MODEL=$VIDEO_MODEL RESOLUTION=$RESOLUTION DURATION=$DURATION"
    exit 1
fi

green() { printf "\033[32m%s\033[0m" "$1"; }
red()   { printf "\033[31m%s\033[0m" "$1"; }
bold()  { printf "\033[1m%s\033[0m" "$1"; }

PASS=0
step() { echo ""; echo "$(bold "▶ $1")"; }
ok()   { echo "  $(green PASS) $1"; PASS=$((PASS + 1)); }
die() {
    echo "  $(red FAIL) $1"
    [ $# -gt 1 ] && echo "       $2"
    echo ""
    echo "$(red "✗ 测试失败")（已通过 $PASS 步）"
    exit 1
}

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

asset_api() { # $1=Action $2=body
    curl -sS -X POST "$BASE/v1/seedance/asset/$1" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
        --max-time 60 -d "$2"
}

echo "$(bold "Seedance 素材库 + 视频生成 端到端测试")"
echo "  BASE=$BASE"
echo "  MODEL=$VIDEO_MODEL  $RESOLUTION/${DURATION}s"

# ── 1. 创建素材组 ─────────────────────────────────────────
step "1/5 CreateAssetGroup"
RESP=$(asset_api CreateAssetGroup "{\"Name\":\"e2e-test-$(date +%s)\",\"Description\":\"seedance e2e 自动测试\"}")
STATE=$(printf '%s' "$RESP" | jget 'd["state"]')
GROUP_ID=$(printf '%s' "$RESP" | jget 'd["data"]["Id"]')
{ [ "$STATE" = "1" ] && [ -n "$GROUP_ID" ]; } || die "创建素材组失败" "$RESP"
ok "素材组: $GROUP_ID"

# ── 2. 创建素材（图片） ───────────────────────────────────
step "2/5 CreateAsset (Image)"
RESP=$(asset_api CreateAsset "{\"GroupId\":\"$GROUP_ID\",\"URL\":\"$IMAGE_URL\",\"Name\":\"e2e-img\",\"AssetType\":\"Image\"}")
STATE=$(printf '%s' "$RESP" | jget 'd["state"]')
ASSET_ID=$(printf '%s' "$RESP" | jget 'd["data"]["Id"]')
{ [ "$STATE" = "1" ] && [ -n "$ASSET_ID" ]; } || die "创建素材失败" "$RESP"
ok "素材: $ASSET_ID"

# ── 3. 轮询审核状态 ───────────────────────────────────────
step "3/5 GetAsset 轮询审核（等待 Active，超时 ${ASSET_TIMEOUT}s）"
DEADLINE=$(($(date +%s) + ASSET_TIMEOUT))
STATUS=""
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    RESP=$(asset_api GetAsset "{\"Id\":\"$ASSET_ID\"}")
    STATUS=$(printf '%s' "$RESP" | jget 'd["data"]["Status"]')
    echo "    status=${STATUS:-?}"
    [ "$STATUS" = "Active" ] && break
    [ "$STATUS" = "Failed" ] && die "素材审核失败" "$RESP"
    sleep "$ASSET_POLL_INTERVAL"
done
[ "$STATUS" = "Active" ] || die "素材审核超时（${ASSET_TIMEOUT}s，最后状态: ${STATUS:-?}）"
ok "素材审核通过 (Active)"

# ── 4. 基于素材提交视频生成 ───────────────────────────────
step "4/5 提交视频生成（first_frame = asset://${ASSET_ID}）"
RESP=$(curl -sS -X POST "$BASE/v1/video/generations" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    --max-time 120 -d "{
    \"model\": \"$VIDEO_MODEL\",
    \"content\": [
        {\"type\": \"text\", \"text\": \"让画面自然动起来，镜头缓缓推近，光线柔和\"},
        {\"type\": \"image_url\", \"image_url\": {\"url\": \"asset://$ASSET_ID\"}, \"role\": \"first_frame\"}
    ],
    \"resolution\": \"$RESOLUTION\",
    \"duration\": $DURATION
}")
TASK_ID=$(printf '%s' "$RESP" | jget 'd["task_id"]')
[ -n "$TASK_ID" ] || die "提交视频任务失败" "$RESP"
ok "任务: $TASK_ID"

# ── 5. 轮询任务直至完成 ───────────────────────────────────
step "5/5 轮询任务（超时 ${VIDEO_TIMEOUT}s）"
DEADLINE=$(($(date +%s) + VIDEO_TIMEOUT))
VIDEO_URL=""
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    RESP=$(curl -sS "$BASE/v1/video/generations/$TASK_ID" \
        -H "Authorization: Bearer $TOKEN" --max-time 60)
    T_STATUS=$(printf '%s' "$RESP" | jget 'd["data"]["status"]')
    T_PROGRESS=$(printf '%s' "$RESP" | jget 'd["data"]["progress"]')
    echo "    status=${T_STATUS:-?} progress=${T_PROGRESS:-?}"
    if [ "$T_STATUS" = "SUCCESS" ]; then
        VIDEO_URL=$(printf '%s' "$RESP" | jget 'd["data"]["data"]["content"]["video_url"]')
        break
    fi
    if [ "$T_STATUS" = "FAILURE" ]; then
        REASON=$(printf '%s' "$RESP" | jget 'd["data"]["fail_reason"]')
        die "视频生成失败" "${REASON:-$RESP}"
    fi
    sleep "$VIDEO_POLL_INTERVAL"
done
[ -n "$VIDEO_URL" ] || die "视频生成超时（${VIDEO_TIMEOUT}s）"
ok "视频生成成功"

echo ""
echo "$(green "✓ 全部 $PASS 步通过")"
echo "  素材组:   $GROUP_ID"
echo "  素材:     $ASSET_ID"
echo "  任务:     $TASK_ID"
echo "  视频 URL: $VIDEO_URL"
