package dto

// ---- Seedance 素材 API 请求（仅定义网关需要预校验的字段） ----
//
// 字段为 PascalCase，与上游 zlhub 资产管理 API 一致；网关只做归属预校验，
// 请求体原样透传给上游，无需字段翻译。

// SeedanceCreateAssetRequest CreateAsset 请求
type SeedanceCreateAssetRequest struct {
	GroupId   string `json:"GroupId"`        // 所属素材组 ID（转发前校验归属）
	URL       string `json:"URL"`            // 素材公网访问地址
	Name      string `json:"Name,omitempty"` // 素材名称
	AssetType string `json:"AssetType"`      // Image / Video / Audio
}

// SeedanceGetAssetRequest GetAsset 请求
type SeedanceGetAssetRequest struct {
	Id string `json:"Id"` // 素材 ID（转发前校验归属）
}

// ---- Seedance 素材 API 响应（zlhub 资产管理 API） ----
//
// 成功（HTTP 200）：{"ResponseMetadata":{...}, "Result":{...}}
// 失败（HTTP 4xx/5xx）：{"Code":"ErrorCode", "Message":"描述", "TrackId":"xxx"}
//
// 与旧 hanyu 上游的 {state,data,error} 外壳不同：成功/失败由 HTTP 状态码区分，
// 网关据此分流，业务数据在 Result 中，错误信息在顶层 Code/Message。

// SeedanceAssetEnvelope 上游成功响应外壳（HTTP 200）。
type SeedanceAssetEnvelope[T any] struct {
	Result T `json:"Result"`
}

// SeedanceAssetError 上游错误响应（HTTP 4xx/5xx）。
type SeedanceAssetError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
	TrackId string `json:"TrackId"`
}

// SeedanceCreateAssetGroupResult CreateAssetGroup 的 Result
type SeedanceCreateAssetGroupResult struct {
	Id string `json:"Id"` // 素材组 ID，如 "group-2026xxx"
}

// SeedanceCreateAssetResult CreateAsset 的 Result
type SeedanceCreateAssetResult struct {
	Id string `json:"Id"` // 素材 ID，如 "asset-2026xxx"
}
