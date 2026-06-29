# Seedance 2.0 视频生成 API

本网关基于 Seedance 2.0 / Seedance 2.0 Fast 提供视频生成能力，支持文生视频、图生视频、首尾帧、参考图/视频/音频等多模态生成。接口为异步模式：提交任务后立即返回任务 ID，再通过查询接口或回调获取结果。

---

## 一、生成模式

| 生成模式 | 输入 | 典型应用 |
|---|---|---|
| 文生视频 | 纯文本 | 创意短片、广告、概念验证 |
| 首帧图生视频 | 文本 + 首帧图 | 从静态画面延展动态场景 |
| 首尾帧生视频 | 文本 + 首帧图 + 尾帧图 | 明确起始与结束的镜头切换 |
| 参考图生视频 | 文本 + N 张参考图 | 角色/场景一致性控制 |
| 视频续写 | 文本 + 源视频 | 扩展已有视频片段 |
| 视频编辑 | 文本 + 源视频 + 参考图 | 替换视频中的物体、保留原机位 |
| 多模态合成 | 文本 + 图 + 视频 + 音频 | 带背景音乐/音效的完整视频 |

---

## 二、鉴权与 Base URL

所有接口统一使用 Bearer Token（你的 API Key）：

```http
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

| 接口 | 方法 | 路径 |
|---|---|---|
| 提交任务 | POST | `{BASE_URL}/v1/video/generations` |
| 查询任务 | GET | `{BASE_URL}/v1/video/generations/{task_id}` |

> 示例中 `BASE_URL` 指你的服务域名，例如 `https://www.aigclink.cc`。

---

## 三、模型

| 模型名 | 描述 | 适用场景 |
|---|---|---|
| `doubao-seedance-2.0` | Seedance 2.0 标准版 | 最佳画质、复杂镜头，支持 480p/720p/1080p |
| `doubao-seedance-2.0-fast` | Seedance 2.0 Fast | 低延迟、成本敏感，支持 480p/720p（不支持 1080p） |

两个模型请求格式完全一致，直接替换 `model` 字段即可。

---

## 四、提交任务

```http
POST {BASE_URL}/v1/video/generations
```

### 4.1 请求体

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `model` | string | 是 | 模型名，见第三节 |
| `content` | array | 是 | 多模态输入数组，顺序决定角色分配 |
| `metadata` | object | 否 | 生成参数（分辨率、时长等），见 4.4 |

> 兼容说明：也支持旧式 `prompt`（文本）+ `images`（图片 URL 数组）+ `metadata.content`。推荐使用顶层 `content[]`。

### 4.2 content 数组元素

每个元素以 `type` 指定类型：

| `type` | 子字段 | 说明 |
|---|---|---|
| `text` | `text` | 提示词，建议放在数组首位 |
| `image_url` | `image_url.url` + `role` | 图片输入（推荐 `asset://{assetId}`） |
| `video_url` | `video_url.url` + `role` | 视频输入（推荐 `asset://{assetId}`） |
| `audio_url` | `audio_url.url` + `role` | 音频输入（推荐 `asset://{assetId}`） |
| `draft_task` | `draft_task.id` | 样片任务（仅 Seedance 1.5 Pro，不可与其它类型混用） |

`image_url.url` / `video_url.url` / `audio_url.url` 支持三种写法：

| 写法 | 说明 |
|---|---|
| 公网 URL | 公网可访问的 HTTPS 地址 |
| 素材 ID | `asset://{assetId}`，引用素材库已审核素材（推荐，见《素材库管理 API》） |
| Base64 | `data:image/png;base64,...`（仅图片，体积受限，不推荐） |

### 4.3 role 取值

| 媒体 | role | 含义 |
|---|---|---|
| 图片 | `first_frame` | 视频首帧（单图，role 不填时默认按首帧处理） |
| 图片 | `last_frame` | 视频尾帧（与 first_frame 搭配做首尾帧） |
| 图片 | `reference_image` | 角色/物体/场景视觉参考（可多张） |
| 视频 | `reference_video` | 续写/编辑的源视频 |
| 音频 | `reference_audio` | 背景音乐或音效 |

混用规则（违反返回 400）：
- `reference_image` 不能与 `first_frame` / `last_frame` 同时出现
- `audio_url` 必须至少搭配一个 `image_url` 或 `video_url`
- 使用 `reference_audio` 时 `metadata.generate_audio` 必须为 `true`

### 4.4 metadata 字段

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `resolution` | string | `720p` | `480p` / `720p` / `1080p`（fast 不支持 1080p） |
| `ratio` | string | `adaptive` | `16:9` / `9:16` / `1:1` / `4:3` / `adaptive` |
| `duration` | int | `5` | 时长（秒），范围 [4,15]，或 `-1`（模型自定） |
| `frames` | int | - | 帧数，与 duration 二选一，frames 优先 |
| `seed` | int | 随机 | 随机种子，相同种子+相同输入结果相近 |
| `generate_audio` | bool | `false` | 是否生成/合成音频（用 reference_audio 时须为 true） |
| `camera_fixed` | bool | `false` | 是否固定机位 |
| `watermark` | bool | `false` | 是否加水印 |
| `return_last_frame` | bool | `false` | 是否返回末帧图 URL |
| `callback_url` | string | - | 任务完成回调地址 |
| `service_tier` | string | `default` | 服务等级 |
| `execution_expires_after` | int | `172800` | 任务超时（秒），范围 [3600, 259200] |

### 4.5 提交响应

成功返回（HTTP 200）：

```json
{
  "id": "task_xxxxxxxxxxxxxxxxxx",
  "task_id": "task_xxxxxxxxxxxxxxxxxx",
  "object": "video",
  "model": "doubao-seedance-2.0",
  "status": "queued",
  "progress": 0,
  "created_at": 1782724287
}
```

`task_id` 是平台任务 ID（轮询凭证）。平台内部已将上游真实任务 ID 抽象隔离，对外统一使用平台 `task_id`，查询/状态均以平台任务为准。

错误返回（HTTP 4xx/5xx）：

```json
{
  "code": "invalid_request_error",
  "message": "the parameter duration specified in the request is not valid",
  "data": null
}
```

### 4.6 示例

文生视频：

```bash
curl -X POST "$BASE_URL/v1/video/generations" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2.0",
    "content": [{"type": "text", "text": "金色猎犬在金秋麦田奔跑，航拍视角，电影级画面"}],
    "metadata": {"duration": 5, "resolution": "720p", "ratio": "16:9"}
  }'
```

首帧图生视频（引用素材库素材）：

```bash
curl -X POST "$BASE_URL/v1/video/generations" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2.0",
    "content": [
      {"type": "text", "text": "让画面自然动起来，镜头缓缓推近"},
      {"type": "image_url", "image_url": {"url": "asset://asset-2026xxxx"}, "role": "first_frame"}
    ],
    "metadata": {"duration": 10, "resolution": "720p", "ratio": "adaptive"}
  }'
```

首尾帧生视频：

```bash
curl -X POST "$BASE_URL/v1/video/generations" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2.0",
    "content": [
      {"type": "text", "text": "从首帧平滑过渡到尾帧"},
      {"type": "image_url", "image_url": {"url": "asset://asset-first"}, "role": "first_frame"},
      {"type": "image_url", "image_url": {"url": "asset://asset-last"}, "role": "last_frame"}
    ],
    "metadata": {"duration": 5, "resolution": "720p", "ratio": "16:9"}
  }'
```

参考图生视频（多图保持角色一致）：

```bash
curl -X POST "$BASE_URL/v1/video/generations" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2.0",
    "content": [
      {"type": "text", "text": "图1和图2中的两个人物在森林中奔跑，自然光，电影质感"},
      {"type": "image_url", "image_url": {"url": "asset://asset-a"}, "role": "reference_image"},
      {"type": "image_url", "image_url": {"url": "asset://asset-b"}, "role": "reference_image"}
    ],
    "metadata": {"duration": 8, "resolution": "720p", "ratio": "16:9"}
  }'
```

视频续写：

```bash
curl -X POST "$BASE_URL/v1/video/generations" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2.0",
    "content": [
      {"type": "text", "text": "延续源视频的运镜与风格"},
      {"type": "video_url", "video_url": {"url": "asset://asset-video"}, "role": "reference_video"}
    ],
    "metadata": {"duration": 8, "resolution": "720p", "ratio": "adaptive"}
  }'
```

多模态合成（图 + 视频 + 音频）：

```bash
curl -X POST "$BASE_URL/v1/video/generations" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2.0",
    "content": [
      {"type": "text", "text": "第一人称视角果茶广告，参考视频的运镜，使用音频作为背景音乐"},
      {"type": "image_url", "image_url": {"url": "asset://asset-img"}, "role": "reference_image"},
      {"type": "video_url", "video_url": {"url": "asset://asset-vid"}, "role": "reference_video"},
      {"type": "audio_url", "audio_url": {"url": "asset://asset-aud"}, "role": "reference_audio"}
    ],
    "metadata": {"duration": 8, "resolution": "720p", "ratio": "16:9", "generate_audio": true}
  }'
```

---

## 五、查询任务

```http
GET {BASE_URL}/v1/video/generations/{task_id}
```

`{task_id}` 为提交时返回的平台任务 ID。状态变化：`SUBMITTED` → `IN_PROGRESS` → `SUCCESS` / `FAILURE`。

成功响应（HTTP 200）：

```json
{
  "code": "success",
  "data": {
    "task_id": "task_xxxxxxxxxxxxxxxxxx",
    "status": "SUCCESS",
    "progress": "100%",
    "submit_time": 1782724287,
    "start_time": 1782724290,
    "finish_time": 1782724520,
    "data": {
      "status": "succeeded",
      "duration": 5,
      "resolution": "720p",
      "ratio": "16:9",
      "framespersecond": 24,
      "content": { "video_url": "https://.../output.mp4?sig=..." },
      "usage": { "completion_tokens": 108900, "total_tokens": 108900 }
    }
  }
}
```

失败时 `data.status` 为 `FAILURE`，`data.fail_reason` 给出原因；任务不存在返回 HTTP 404 `{"error":{"code":"NotFound","message":"...","type":"new_api_error"}}`。

### 状态字段

| `data.status`（外层，建议使用） | 含义 |
|---|---|
| `SUBMITTED` | 已提交，未开始 |
| `IN_PROGRESS` | 生成中 |
| `SUCCESS` | 成功，视频可下载 |
| `FAILURE` | 失败，查看 `fail_reason` |

### 轮询建议

- 间隔 10–15 秒；文生视频约 30s–3min，参考/编辑约 2–8min
- 客户端超时建议 ≥ 10 分钟，或在 `metadata.callback_url` 配置回调避免轮询
- 视频 URL 为临时签名链接，请及时下载或转存

---

## 六、计费

按 token 用量计费，受以下因素影响：

- `duration`：时长越长用量越高
- `resolution`：`1080p` 相比 `720p` 额外计费
- 含视频输入（图生视频/视频生视频）：按含视频输入单价计算，低于纯文生视频单价

最终单价以平台模型配置为准；任务失败时预扣费自动退还。

---

## 七、约束

- 时长：整数秒 [4,15] 或 `-1`
- 参考音频时长不得超过视频时长；`audio_url` 不能单独输入
- `reference_image` 与 `first_frame`/`last_frame` 互斥；`first_frame` 与 `last_frame` 可同时用
- 所有 URL 须公网 HTTPS 可达、Content-Type 正确；推荐使用素材库 `asset://` 引用
- 所有文本与素材均经内容审核，命中策略将以 `FAILURE` 结束

---

## 八、错误码

| HTTP | 含义 | 处理建议 |
|---|---|---|
| 400 | 参数错误 | 检查 content / metadata / 互斥规则 |
| 401 | 鉴权失败 | 检查 Authorization 是否带 Token |
| 403 | 无权限 | 确认 Token 是否有该模型权限 |
| 404 | 任务不存在 | 检查 task_id |
| 429 | 限流 | 指数退避后重试 |
| 500 | 服务端错误 | 稍后重试 |
