# HappyHorse 视频生成 API

本网关提供 HappyHorse 系列四个视频模型：文生视频、图生视频、参考生视频与视频编辑。接口为异步模式：提交任务后立即返回任务 ID，再通过查询接口轮询结果。

---

## 一、鉴权与 Base URL

所有接口统一使用 Bearer Token（你的 API Key）：

```http
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

| 接口 | 方法 | 路径 |
|---|---|---|
| 提交任务 | POST | `{BASE_URL}/v1/video/generations` |
| 查询任务 | GET | `{BASE_URL}/v1/video/generations/{task_id}` |

> 示例中 `BASE_URL` 指你的服务域名，例如 `https://opclink.cc`。

---

## 二、模型与价格

| 模型名 | 能力 | 480P | 720P | 1080P |
|---|---|---|---|---|
| `happyhorse-1.1-t2v` | 文生视频 | ¥0.27/秒 | ¥0.54/秒 | ¥0.72/秒 |
| `happyhorse-1.1-i2v` | 图生视频（首帧） | ¥0.27/秒 | ¥0.54/秒 | ¥0.72/秒 |
| `happyhorse-1.1-r2v` | 参考生视频（1–9 张参考图） | ¥0.27/秒 | ¥0.54/秒 | ¥0.72/秒 |
| `happyhorse-1.0-video-edit` | 视频编辑（指令改写视频） | — | ¥0.72/秒 | ¥1.28/秒 |

- 计费 = 每秒单价 × 时长，提交时预扣，任务失败自动全额退款。
- **video-edit 不传时长**（输出跟随输入视频），提交时按 5 秒预估预扣，完成后按上游实际用量差额结算（多退少补）。

---

## 三、通用参数（metadata）

| 参数 | 类型 | 说明 |
|---|---|---|
| `resolution` | string | `480P` / `720P` / `1080P`，默认 `1080P`（video-edit 无 480P） |
| `ratio` | string | 宽高比，如 `16:9`、`9:16`、`1:1` |
| `duration` | int | 时长 3–15 秒（video-edit 不支持该参数） |
| `watermark` | bool | 是否加水印，本网关默认 `false` |
| `seed` | int | 随机种子（可选） |

媒体素材通过 `metadata.media` 数组传入，`type` 取值：`first_frame`（首帧图）、`reference_image`（参考图）、`video`（待编辑视频）。i2v 也可直接用顶层 `image` 字段。

---

## 四、curl 示例

### 1. 文生视频（t2v）

```bash
curl "$baseurl/v1/video/generations" \
  -H "Authorization: Bearer $apikey" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "happyhorse-1.1-t2v",
    "prompt": "一只橘猫在洒满阳光的窗台上打盹，微风吹动窗帘，温馨治愈风格",
    "metadata": { "resolution": "480P", "ratio": "16:9", "duration": 3 }
  }'
```

### 2. 图生视频（i2v，首帧图）

```bash
curl "$baseurl/v1/video/generations" \
  -H "Authorization: Bearer $apikey" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "happyhorse-1.1-i2v",
    "prompt": "镜头缓缓拉近，人物微笑回头",
    "image": "https://example.com/first-frame.jpg",
    "metadata": { "resolution": "720P", "duration": 5 }
  }'
```

### 3. 参考生视频（r2v，多参考图）

```bash
curl "$baseurl/v1/video/generations" \
  -H "Authorization: Bearer $apikey" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "happyhorse-1.1-r2v",
    "prompt": "参考图中的角色在雪地里奔跑",
    "metadata": {
      "resolution": "720P",
      "duration": 5,
      "media": [
        { "type": "reference_image", "url": "https://example.com/ref1.jpg" },
        { "type": "reference_image", "url": "https://example.com/ref2.jpg" }
      ]
    }
  }'
```

### 4. 视频编辑（video-edit，不传时长）

```bash
curl "$baseurl/v1/video/generations" \
  -H "Authorization: Bearer $apikey" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "happyhorse-1.0-video-edit",
    "prompt": "把视频中的天空替换成晚霞，整体色调改为暖色",
    "metadata": {
      "resolution": "720P",
      "media": [
        { "type": "video", "url": "https://example.com/input.mp4" },
        { "type": "reference_image", "url": "https://example.com/style.jpg" }
      ]
    }
  }'
```

### 5. 查询任务

```bash
curl "$baseurl/v1/video/generations/{task_id}" \
  -H "Authorization: Bearer $apikey"
```

---

## 五、任务状态与结果

查询返回 `{"code":"success","data":{...}}`，`data.status` 取值：

| 状态 | 含义 |
|---|---|
| `NOT_START` / `SUBMITTED` / `QUEUED` | 排队中 |
| `IN_PROGRESS` | 生成中 |
| `SUCCESS` | 成功，`data.data.output.video_url` 为视频地址 |
| `FAILURE` | 失败，`data.fail_reason` 为原因；预扣费用自动全额退款 |

视频链接有时效性，请及时转存。
