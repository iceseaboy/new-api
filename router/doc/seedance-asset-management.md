# Seedance 素材库管理 API

素材库用于在视频生成前管理图片/视频/音频素材：上传后经审核进入可用状态，再在视频生成请求中以 `asset://{assetId}` 引用。素材数据按账号严格隔离，不同账号无法互访。

> **素材库接口完全免费**（不计费、不预扣费），与具体生成模型无关。

---

## 一、鉴权与 Base URL

所有接口统一使用 Bearer Token（你的 API Key）：

```http
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

| 接口 | 方法 | 路径 | 说明 |
|---|---|---|---|
| 创建素材组 | POST | `{BASE_URL}/v1/seedance/asset/CreateAssetGroup` | 先建组，再传素材 |
| 上传素材 | POST | `{BASE_URL}/v1/seedance/asset/CreateAsset` | 素材须挂在已有素材组下 |
| 查询素材 | POST | `{BASE_URL}/v1/seedance/asset/GetAsset` | 按素材 ID 查状态与访问地址 |
| 本地素材列表 | GET | `{BASE_URL}/v1/seedance/assets` | 查询本账号素材归属记录 |

> 示例中 `BASE_URL` 指你的服务域名，例如 `https://www.aigclink.cc`。

---

## 二、响应格式

所有素材接口的成功响应统一包装为 `{state, data, error}`，`state=1` 且 `error=null` 表示成功：

```json
{ "state": 1, "data": { ... }, "error": null }
```

错误响应（HTTP 4xx）为 `{error}` 对象：

```json
{
  "error": {
    "message": "素材组不属于当前用户",
    "type": "new_api_error",
    "code": "access_denied"
  }
}
```

---

## 三、典型使用流程

```
1. CreateAssetGroup  → 拿到 GroupId
2. CreateAsset       → 绑定 GroupId + 素材 URL → 拿到 AssetId
3. GetAsset（轮询）  → 等待 Status 变为 Active
4. 视频生成请求中引用：asset://{assetId}
```

---

## 四、CreateAssetGroup — 创建素材组

```http
POST {BASE_URL}/v1/seedance/asset/CreateAssetGroup
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `Name` | string | 是 | 素材组名称 |
| `Description` | string | 否 | 描述备注 |

请求示例：

```bash
curl -X POST "$BASE_URL/v1/seedance/asset/CreateAssetGroup" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{"Name": "广告素材组", "Description": "Q2 产品广告参考素材"}'
```

成功响应：

```json
{
  "state": 1,
  "data": { "Id": "group-20260629171111-4m7p6", "Name": "广告素材组" },
  "error": null
}
```

---

## 五、CreateAsset — 上传素材

```http
POST {BASE_URL}/v1/seedance/asset/CreateAsset
```

`URL` 用于把你的原始素材导入素材库（公网 HTTPS 地址，服务端拉取并审核/转码）。导入成功后返回 `AssetId`，在视频生成请求里用 `asset://{assetId}` 引用。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `GroupId` | string | 是 | 目标素材组 ID（须为本账号创建） |
| `URL` | string | 是 | 素材公网可访问地址 |
| `AssetType` | string | 是 | `Image` / `Video` / `Audio` |
| `Name` | string | 否 | 素材名称 |

请求示例：

```bash
curl -X POST "$BASE_URL/v1/seedance/asset/CreateAsset" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{
    "GroupId": "group-20260629171111-4m7p6",
    "URL": "https://your-cdn.example.com/reference.jpg",
    "AssetType": "Image",
    "Name": "主角参考图"
  }'
```

成功响应（素材进入处理中，需轮询 GetAsset）：

```json
{
  "state": 1,
  "data": { "Id": "asset-20260629171116-lvkpc" },
  "error": null
}
```

`GroupId` 不属于当前账号时返回 HTTP 403：

```json
{ "error": { "message": "素材组不属于当前用户", "type": "new_api_error", "code": "access_denied" } }
```

---

## 六、GetAsset — 查询素材

```http
POST {BASE_URL}/v1/seedance/asset/GetAsset
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `Id` | string | 是 | 素材 ID |

请求示例：

```bash
curl -X POST "$BASE_URL/v1/seedance/asset/GetAsset" \
  -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{"Id": "asset-20260629171116-lvkpc"}'
```

成功响应：

```json
{
  "state": 1,
  "data": {
    "Id": "asset-20260629171116-lvkpc",
    "Name": "主角参考图",
    "AssetType": "Image",
    "GroupId": "group-20260629171111-4m7p6",
    "Status": "Active",
    "URL": "https://.../reference.png?X-Tos-Algorithm=...",
    "CreateTime": "2026-06-29T09:11:16Z",
    "UpdateTime": "2026-06-29T09:11:19Z"
  },
  "error": null
}
```

素材不属于当前账号时返回 HTTP 403。

### 素材状态

| Status | 含义 | 建议 |
|---|---|---|
| `Processing` | 审核与处理中 | 继续轮询 |
| `Active` | 审核通过，可用于视频生成 | 使用返回的 `URL` 或以 `asset://{Id}` 引用 |
| `Failed` | 审核失败 | 查看 `Error` 字段，换素材重试 |

> `Status=Active` 后 `URL` 为带时效签名的访问地址；轮询间隔建议 5–15 秒，图片通常 < 30 秒完成。

---

## 七、本地素材列表

```http
GET {BASE_URL}/v1/seedance/assets?page=1&page_size=20
```

查询本账号素材的本地归属记录（不调用上游、不含实时状态，实时状态请用 GetAsset）。

```bash
curl "$BASE_URL/v1/seedance/assets?page=1&page_size=20" -H "Authorization: Bearer $API_KEY"
```

响应：

```json
{
  "data": [
    {
      "id": 1,
      "user_id": 1,
      "channel_id": 6,
      "asset_id": "asset-20260629171116-lvkpc",
      "asset_group_id": "group-20260629171111-4m7p6",
      "asset_type": "image",
      "created_at": 1782724276
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

## 八、在视频生成中引用素材

素材 `Status=Active` 后，在视频生成请求的 `content` 里以 `asset://{assetId}` 引用：

```json
{
  "type": "image_url",
  "image_url": { "url": "asset://asset-20260629171116-lvkpc" },
  "role": "reference_image"
}
```

详见《Seedance 2.0 视频生成 API》。

---

## 九、错误码

| code | HTTP | 说明 |
|---|---|---|
| `invalid_request` | 400 | 参数缺失或格式错误（如 GroupId/URL/Id 为空） |
| `access_denied` | 403 | 素材组/素材不属于当前账号 |
| `bad_response_status_code` | 5xx | 上游素材服务异常，可稍后重试 |

---

## 十、完整示例（Python）

```python
import time, requests

BASE_URL = "https://www.aigclink.cc"
API_KEY  = "sk-your-api-key"
H = {"Authorization": f"Bearer {API_KEY}", "Content-Type": "application/json"}

def call(action, body):
    r = requests.post(f"{BASE_URL}/v1/seedance/asset/{action}", json=body, headers=H)
    return r.json()

# 1. 创建素材组
gid = call("CreateAssetGroup", {"Name": "我的素材组"})["data"]["Id"]

# 2. 上传素材
aid = call("CreateAsset", {
    "GroupId": gid,
    "URL": "https://your-cdn.example.com/photo.png",
    "AssetType": "Image",
    "Name": "示例图片"
})["data"]["Id"]

# 3. 轮询至 Active
for _ in range(20):
    time.sleep(5)
    data = call("GetAsset", {"Id": aid})["data"]
    if data["Status"] in ("Active", "Failed"):
        break

# 4. 在视频生成中以 asset://{aid} 引用
print("引用地址:", f"asset://{aid}")
```
