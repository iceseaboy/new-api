# Kling 视频生成 API

本网关基于 Kling（可灵）系列模型提供文生视频与图生视频能力。接口为异步模式：提交任务后立即返回任务 ID，再通过查询接口轮询结果。

---

## 一、鉴权与 Base URL

所有接口统一使用 Bearer Token（你的 API Key）：

```http
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

| 接口 | 方法 | 路径 |
|---|---|---|
| 文生视频提交 | POST | `{BASE_URL}/kling/v1/videos/text2video` |
| 文生视频查询 | GET | `{BASE_URL}/kling/v1/videos/text2video/{task_id}` |
| 图生视频提交 | POST | `{BASE_URL}/kling/v1/videos/image2video` |
| 图生视频查询 | GET | `{BASE_URL}/kling/v1/videos/image2video/{task_id}` |
| 视频下载（代理） | GET | `{BASE_URL}/v1/videos/{task_id}/content` |

> 示例中 `BASE_URL` 指你的服务域名，例如 `https://opclink.cc`。

---

## 二、模型与价格

| 模型名 | 计费方式 | std（720P） | pro（1080P） |
|---|---|---|---|
| `kling-v2-5-turbo` | 按输出秒计费 | ¥0.3024/秒 | ¥0.504/秒 |

计费 = 每秒单价 × `duration` 秒数，提交时预扣，任务失败自动全额退款。
示例：std 5 秒 = ¥1.512；pro 10 秒 = ¥5.04。

---

## 三、提交任务

### 3.1 请求体（文生视频）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `model_name` | string | 是 | 模型名，如 `kling-v2-5-turbo`（兼容旧字段名 `model`） |
| `prompt` | string | 是 | 提示词，≤2500 字符 |
| `negative_prompt` | string | 否 | 负向提示词 |
| `duration` | string | 否 | 时长秒数字符串，`"5"` / `"10"`，默认 `"5"` |
| `mode` | string | 否 | `std`（720P，默认）/ `pro`（1080P） |
| `aspect_ratio` | string | 否 | `16:9`（默认）/ `9:16` / `1:1` |

> 注意：`cfg_scale` 参数 kling-v2.x 模型不支持，请勿传入。

### 3.2 请求体（图生视频，增量字段）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `image` | string | 二选一 | 首帧参考图：公网 URL 或 Base64（无前缀）。≤10MB，宽高≥300px，宽高比 1:2.5~2.5:1 |
| `image_tail` | string | 二选一 | 尾帧参考图，与 `image` 至少一个 |

其余字段同文生视频。

### 3.3 提交响应

```json
{
  "id": "task_xxxxxxxxxxxxxxxxxx",
  "task_id": "task_xxxxxxxxxxxxxxxxxx",
  "object": "video",
  "model": "kling-v2-5-turbo",
  "status": "queued",
  "progress": 0,
  "created_at": 1785858972
}
```

`task_id` 为平台任务 ID（轮询凭证）。

---

## 四、查询任务

```http
GET {BASE_URL}/kling/v1/videos/text2video/{task_id}
```

（图生视频用 `image2video` 路径，`task_id` 为提交时返回值。）

响应（成功态节选）：

```json
{
  "code": "success",
  "data": {
    "task_id": "task_xxxxxxxxxxxxxxxxxx",
    "status": "SUCCESS",
    "progress": "100%",
    "result_url": "https://opclink.cc/v1/videos/task_xxx/content",
    "fail_reason": ""
  }
}
```

| 字段 | 说明 |
|---|---|
| `data.status` | `NOT_START` → `IN_PROGRESS` → `SUCCESS` / `FAILURE` |
| `data.progress` | 进度百分比 |
| `data.result_url` | 成功后的视频下载地址（网关代理，直接 GET 携带同样的 Bearer 鉴权） |
| `data.fail_reason` | 失败原因（失败时预扣费自动退回） |

### 视频下载

```bash
curl -L "$BASE_URL/v1/videos/{task_id}/content" \
  -H "Authorization: Bearer $API_KEY" -o video.mp4
```

---

## 五、示例

文生视频（std 720P，5 秒）：

```bash
curl -X POST "$BASE_URL/kling/v1/videos/text2video" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model_name": "kling-v2-5-turbo",
    "prompt": "一只橘猫在洒满阳光的窗台上伸懒腰，毛发细节清晰，电影质感",
    "duration": "5",
    "mode": "std",
    "aspect_ratio": "16:9"
  }'
```

文生视频（pro 1080P，竖屏 10 秒）：

```bash
curl -X POST "$BASE_URL/kling/v1/videos/text2video" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model_name": "kling-v2-5-turbo",
    "prompt": "深夜城市雨景，霓虹倒影湿润街面，行人撑透明伞走过，电影感",
    "duration": "10",
    "mode": "pro",
    "aspect_ratio": "9:16"
  }'
```

图生视频（首帧图驱动）：

```bash
curl -X POST "$BASE_URL/kling/v1/videos/image2video" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "model_name": "kling-v2-5-turbo",
    "image": "https://example.com/first-frame.png",
    "prompt": "让画面自然动起来，镜头缓缓推近，光线柔和",
    "duration": "5",
    "mode": "std",
    "aspect_ratio": "16:9"
  }'
```

查询 + 下载：

```bash
curl "$BASE_URL/kling/v1/videos/text2video/task_xxx" \
  -H "Authorization: Bearer $API_KEY"

curl -L "$BASE_URL/v1/videos/task_xxx/content" \
  -H "Authorization: Bearer $API_KEY" -o video.mp4
```

---

## 六、错误与退款

| 情况 | 行为 |
|---|---|
| 参数错误（如 v2.x 传 `cfg_scale`） | 任务 `FAILURE`，`fail_reason` 说明原因，预扣费自动退回 |
| 上游拒单 / 生成失败 | 同上，自动退款 |
| 查询他人任务 | 404（任务按用户隔离） |
