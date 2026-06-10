package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ---- Seedance 素材 API 请求（仅定义网关需要预校验的字段） ----

// SeedanceCreateAssetRequest CreateAsset 请求
type SeedanceCreateAssetRequest struct {
	GroupId   string `json:"GroupId"`        // 所属素材组 ID（转发前校验归属）
	URL       string `json:"URL"`            // 素材公共访问地址
	Name      string `json:"Name,omitempty"` // 素材名称
	AssetType string `json:"AssetType"`      // Image / Video / Audio
}

// SeedanceGetAssetRequest GetAsset 请求
type SeedanceGetAssetRequest struct {
	Id string `json:"Id"` // 素材 ID（转发前校验归属）
}

// ---- Seedance 素材 API 响应 ----

// SeedanceAPIResponse 上游统一外层响应。
//
// 观察到的上游实际返回形态（至少三种）：
//
//  1. 业务成功：
//     {"state":1, "data":{"Id":"...", ...}, "error":null}
//  2. 业务级失败（外壳是 state=1，但 data 里嵌业务错误对象）：
//     {"state":1, "data":{"Code":"InvalidParameter.XXX","Message":"...","Data":null}, "error":null}
//  3. Framework 级失败（state=0，data=null，error 是字符串数组）：
//     {"state":0, "data":null, "error":["ID不能为空"]}
//
// Error 字段使用 json.RawMessage 是因为上游 error 字段的 JSON shape 在不同错误
// 场景下并不固定（null / string array / object 均有可能）。调用方应通过
// IsFrameworkError / ErrorMessage 方法判断与取值，不要直接 assertion。
type SeedanceAPIResponse[T any] struct {
	State int             `json:"state"`
	Data  T               `json:"data"`
	Error json.RawMessage `json:"error"`
}

// IsFrameworkError 判断上游是否返回了 framework 级错误（形态 3）。
// 判定条件：state != 1，或 error 字段非空且非 JSON null。
func (r *SeedanceAPIResponse[T]) IsFrameworkError() bool {
	if r.State != 1 {
		return true
	}
	if len(r.Error) == 0 {
		return false
	}
	s := strings.TrimSpace(string(r.Error))
	return s != "" && s != "null"
}

// ErrorMessage best-effort 地从 error 字段提取人类可读的错误消息。
// 兼容多种上游 shape：
//   - JSON null / 空 → 返回 ""
//   - string array（如 ["ID不能为空"]）→ 用 "; " 拼接
//   - single string（"some error"）→ 原样返回
//   - 其他 shape → 返回原始 JSON 片段
func (r *SeedanceAPIResponse[T]) ErrorMessage() string {
	if len(r.Error) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(r.Error))
	if s == "" || s == "null" {
		return ""
	}
	var arr []string
	if common.Unmarshal(r.Error, &arr) == nil {
		return strings.Join(arr, "; ")
	}
	var str string
	if common.Unmarshal(r.Error, &str) == nil {
		return str
	}
	return s
}

// SeedanceCreateAssetGroupResponse CreateAssetGroup data 字段
type SeedanceCreateAssetGroupResponse struct {
	Id string `json:"Id"` // 素材组 ID，如 "group-2026xxx"
}

// SeedanceCreateAssetResponse CreateAsset data 字段
type SeedanceCreateAssetResponse struct {
	Id string `json:"Id"` // 素材 ID，如 "asset-2026xxx"
}

// SeedanceAssetError 上游业务错误对象（嵌在 data 内）
type SeedanceAssetError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}
