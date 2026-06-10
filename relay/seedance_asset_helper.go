package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// SeedanceAssetHelper 处理 Seedance 素材资产 API 的透传、多租户校验、ownership 记录。
// 素材接口免费（不计费、不预扣费），与具体生成模型无关。
// 三个端点统一入口：
//   - CreateAssetGroup: 转发上游 → 上游成功后入 DB → 返回响应
//   - CreateAsset: 先校验 group 归属 → 转发上游 → 上游成功后入 DB → 返回响应
//   - GetAsset: 先校验 asset 归属 → 透传上游 → 直接返回响应，不写 DB
func SeedanceAssetHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)

	endpoint := identifySeedanceEndpoint(c.Request.URL.Path)
	if endpoint == "" {
		return types.NewErrorWithStatusCode(fmt.Errorf("未知的 Seedance 素材端点: %s", c.Request.URL.Path),
			types.ErrorCodeInvalidRequest, http.StatusNotFound, types.ErrOptionWithSkipRetry())
	}
	userId := info.UserId
	tokenId := info.TokenId
	channelId := info.ChannelId

	// ---- 转发前校验（多租户隔离：userId + channelId） ----
	var createAssetReq dto.SeedanceCreateAssetRequest
	switch endpoint {
	case "CreateAsset":
		if err := common.UnmarshalBodyReusable(c, &createAssetReq); err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("请求体 JSON 格式错误: %w", err),
				types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if createAssetReq.GroupId == "" {
			return types.NewErrorWithStatusCode(fmt.Errorf("GroupId 不能为空"),
				types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}

		// 校验 GroupId 归属
		owned, err := model.CheckAssetGroupOwnership(userId, channelId, createAssetReq.GroupId)
		if err != nil {
			logger.LogError(c, fmt.Sprintf("校验 GroupId 归属失败: %s", err.Error()))
			return types.NewError(fmt.Errorf("归属校验服务异常"),
				types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !owned {
			return types.NewErrorWithStatusCode(fmt.Errorf("素材组不属于当前用户"),
				types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}

	case "GetAsset":
		var getReq dto.SeedanceGetAssetRequest
		if err := common.UnmarshalBodyReusable(c, &getReq); err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("请求体 JSON 格式错误: %w", err),
				types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if getReq.Id == "" {
			return types.NewErrorWithStatusCode(fmt.Errorf("Id 不能为空"),
				types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}

		// 校验 AssetId 归属
		asset, err := model.CheckAssetOwnership(userId, channelId, getReq.Id)
		if err != nil {
			logger.LogError(c, fmt.Sprintf("校验 AssetId 归属失败: %s", err.Error()))
			return types.NewError(fmt.Errorf("归属校验服务异常"),
				types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if asset == nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("素材不属于当前用户"),
				types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	}

	// ---- 构建上游请求 ----
	targetURL, err := buildSeedanceAssetURL(c, info)
	if err != nil {
		return types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	bodyStorage, err := common.GetBodyStorage(c)
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("读取请求体失败: %w", err),
			types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	requestBody, err := bodyStorage.Bytes()
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("读取请求体失败: %w", err),
			types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return types.NewError(fmt.Errorf("创建请求失败: %w", err),
			types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}

	// 设置请求头；渠道可通过 header_override 配置自定义鉴权头（支持 {api_key} 占位符）
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
	}
	for k, v := range headerOverride {
		req.Header.Set(k, v)
		if strings.EqualFold(k, "Host") {
			req.Host = v
		}
	}

	// ---- 发送请求到上游 ----
	client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		return types.NewError(fmt.Errorf("创建 HTTP 客户端失败: %w", err),
			types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}

	resp, err := client.Do(req)
	if err != nil {
		return types.NewError(fmt.Errorf("上游请求失败: %w", err), types.ErrorCodeDoRequestFailed)
	}
	defer resp.Body.Close()

	// 读取完整响应（先校验、入库，再写给客户端）
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return types.NewError(fmt.Errorf("读取上游响应失败: %w", readErr),
			types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}

	// HTTP 非 2xx → 可重试错误
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.LogError(c, fmt.Sprintf("Seedance 上游返回 HTTP %d, body=%s", resp.StatusCode, string(body)))
		return types.NewErrorWithStatusCode(
			fmt.Errorf("Seedance 上游返回 HTTP %d", resp.StatusCode),
			types.ErrorCodeBadResponseStatusCode,
			resp.StatusCode,
		)
	}
	logger.LogInfo(c, fmt.Sprintf("Seedance 上游响应 HTTP %d, endpoint=%s, body=%s", resp.StatusCode, endpoint, string(body)))

	// ---- 上游外壳内错误拦截 ----
	// HTTP 2xx 只能证明 framework 层传输成功。上游的外壳 {state, data, error}
	// 可能再嵌两种失败：
	//   - framework 级：state != 1 或 error 非空（例如 {"state":0,"data":null,"error":["ID不能为空"]}）
	//   - 业务级：     state == 1，但 data 内嵌 {Code, Message, Data:null}（例如 URL 下载失败）
	// 不先拦截这两种，后续 handleCreateXxx 的 "Id 为空" 分支会把它们误报成
	// "上游响应格式异常"，掩盖上游真实错误。

	if fwErr, isFwErr := extractSeedanceFrameworkError(body); isFwErr {
		logger.LogError(c, fmt.Sprintf("Seedance 上游 framework 错误: endpoint=%s, error=%s", endpoint, fwErr))
		return types.NewErrorWithStatusCode(
			fmt.Errorf("上游 framework 错误: %s", fwErr),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry())
	}

	if bizCode, bizMsg, isBizErr := extractSeedanceBusinessError(body); isBizErr {
		logger.LogError(c, fmt.Sprintf("Seedance 上游业务错误: endpoint=%s, Code=%s, Message=%s",
			endpoint, bizCode, bizMsg))
		return types.NewErrorWithStatusCode(
			fmt.Errorf("%s: %s", bizCode, bizMsg),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry())
	}

	// ---- 上游成功后：入 DB（创建类端点） ----
	switch endpoint {
	case "CreateAssetGroup":
		if apiErr := handleCreateAssetGroup(c, userId, tokenId, channelId, body); apiErr != nil {
			return apiErr
		}

	case "CreateAsset":
		if apiErr := handleCreateAsset(c, userId, tokenId, channelId, body, createAssetReq.GroupId, createAssetReq.AssetType); apiErr != nil {
			return apiErr
		}

	case "GetAsset":
		// 查询类：直接透传，不写 DB
	}

	// ---- DB 成功 → 写响应给客户端 ----
	writeSeedanceResponse(c, resp, body)

	// ---- postConsume（免费：quota=0，只记日志和请求计数） ----
	postConsumeSeedanceAsset(c, info, endpoint)

	return nil
}

// ---- URL 映射 ----

// buildSeedanceAssetURL 构建上游请求 URL
// /v1/seedance/asset/CreateAssetGroup → {base_url}/asset/CreateAssetGroup
func buildSeedanceAssetURL(c *gin.Context, info *relaycommon.RelayInfo) (string, error) {
	baseURL := info.ChannelBaseUrl
	if baseURL == "" {
		return "", fmt.Errorf("Seedance asset channel base URL 未配置")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// /v1/seedance/asset/CreateAssetGroup → /asset/CreateAssetGroup
	vendorPath := strings.Replace(c.Request.URL.Path, "/v1/seedance", "", 1)

	return baseURL + vendorPath, nil
}

// ---- 响应写入 ----

func writeSeedanceResponse(c *gin.Context, resp *http.Response, body []byte) {
	hopByHopHeaders := map[string]bool{
		"Connection": true, "Keep-Alive": true, "Transfer-Encoding": true,
		"Proxy-Authenticate": true, "Proxy-Authorization": true,
		"Te": true, "Trailer": true, "Upgrade": true, "Set-Cookie": true,
	}
	for key, values := range resp.Header {
		if hopByHopHeaders[http.CanonicalHeaderKey(key)] {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)
	if _, writeErr := c.Writer.Write(body); writeErr != nil {
		logger.LogError(c, fmt.Sprintf("写入响应体失败: %s", writeErr.Error()))
	}
}

// ---- Ownership 处理 ----

func handleCreateAssetGroup(c *gin.Context, userId, tokenId, channelId int, respBody []byte) *types.NewAPIError {
	var outer dto.SeedanceAPIResponse[dto.SeedanceCreateAssetGroupResponse]
	if common.Unmarshal(respBody, &outer) != nil || outer.Data.Id == "" {
		logger.LogError(c, fmt.Sprintf("CreateAssetGroup: 无法从上游响应解析 Id, upstream_body=%s", string(respBody)))
		return types.NewError(fmt.Errorf("上游响应格式异常"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}

	group := &model.VendorAssetGroup{
		UserId:       userId,
		TokenId:      tokenId,
		ChannelId:    channelId,
		AssetGroupId: outer.Data.Id,
		CreatedAt:    time.Now().Unix(),
	}
	alreadyExists, err := group.Insert()
	if err != nil {
		logger.LogError(c, fmt.Sprintf("写入素材组 ownership 失败: group_id=%s, user_id=%d, err=%s",
			outer.Data.Id, userId, err.Error()))
		return types.NewError(fmt.Errorf("素材组归属记录失败，请联系管理员"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if alreadyExists {
		logger.LogWarn(c, fmt.Sprintf("素材组已存在（幂等）: group_id=%s, user_id=%d", outer.Data.Id, userId))
	}

	return nil
}

func handleCreateAsset(c *gin.Context, userId, tokenId, channelId int, respBody []byte, groupId, assetType string) *types.NewAPIError {
	var outer dto.SeedanceAPIResponse[dto.SeedanceCreateAssetResponse]
	if common.Unmarshal(respBody, &outer) != nil || outer.Data.Id == "" {
		logger.LogError(c, fmt.Sprintf("CreateAsset: 无法从上游响应解析 Id, upstream_body=%s", string(respBody)))
		return types.NewError(fmt.Errorf("上游响应格式异常"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}

	// group 归属已在转发前校验通过，直接入 DB
	asset := &model.VendorAsset{
		UserId:       userId,
		TokenId:      tokenId,
		ChannelId:    channelId,
		AssetId:      outer.Data.Id,
		AssetGroupId: groupId,
		AssetType:    strings.ToLower(assetType),
		CreatedAt:    time.Now().Unix(),
	}
	alreadyExists, err := asset.Insert()
	if err != nil {
		logger.LogError(c, fmt.Sprintf("写入素材 ownership 失败: asset_id=%s, group_id=%s, user_id=%d, err=%s",
			outer.Data.Id, groupId, userId, err.Error()))
		return types.NewError(fmt.Errorf("素材归属记录失败，请联系管理员"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if alreadyExists {
		logger.LogWarn(c, fmt.Sprintf("素材已存在（幂等）: asset_id=%s, group_id=%s, user_id=%d",
			outer.Data.Id, groupId, userId))
	}

	return nil
}

// ---- 上游错误检测 ----
//
// 上游 Seedance Asset API 的外壳 {"state":1,"data":{...},"error":null} 可能
// 出现三种形态（详见 dto.SeedanceAPIResponse 的文档注释）。这里抽出两个专职
// 函数，用于在主 helper 里按优先级拦截：
//   1. extractSeedanceFrameworkError → 形态 3（state!=1 或 error 非空）
//   2. extractSeedanceBusinessError  → 形态 2（state=1 但 data 内嵌业务错误）
//
// 设计意图是让业务错误和 framework 错误都能透出上游的真实错误消息给调用方，
// 避免被 handleCreateAsset/handleCreateAssetGroup 里"Id 为空 → 上游响应格式异常"
// 的兜底分支吞掉。

// extractSeedanceFrameworkError 检测 framework 级错误（形态 3）。
// 返回 (错误消息, true) 如果命中；否则 ("", false)。
func extractSeedanceFrameworkError(respBody []byte) (string, bool) {
	// 用 json.RawMessage 作为 data 的 T，这样不管 data 是什么 shape 都不会
	// 在 Unmarshal 阶段失败，让检测函数对任何 data shape 都鲁棒。
	var outer dto.SeedanceAPIResponse[json.RawMessage]
	if common.Unmarshal(respBody, &outer) != nil {
		return "", false
	}
	if !outer.IsFrameworkError() {
		return "", false
	}
	msg := outer.ErrorMessage()
	if msg == "" {
		msg = fmt.Sprintf("state=%d", outer.State)
	}
	return msg, true
}

// extractSeedanceBusinessError 检测业务级错误（形态 2）：state=1 但 data 里
// 嵌了 {Code, Message, Data:null}。返回 (code, message, true) 如果命中；
// 否则 ("", "", false)。
func extractSeedanceBusinessError(respBody []byte) (code, message string, ok bool) {
	var outer dto.SeedanceAPIResponse[dto.SeedanceAssetError]
	if common.Unmarshal(respBody, &outer) != nil {
		return "", "", false
	}
	// framework 错误不在这里处理
	if outer.State != 1 {
		return "", "", false
	}
	if outer.Data.Code == "" {
		return "", "", false
	}
	return outer.Data.Code, outer.Data.Message, true
}

// ---- 端点识别 ----

func identifySeedanceEndpoint(path string) string {
	switch {
	case strings.HasSuffix(path, "/CreateAssetGroup"):
		return "CreateAssetGroup"
	case strings.HasSuffix(path, "/CreateAsset"):
		return "CreateAsset"
	case strings.HasSuffix(path, "/GetAsset"):
		return "GetAsset"
	default:
		return ""
	}
}

// ---- PostConsume（免费：quota=0，只记日志和请求计数） ----

func postConsumeSeedanceAsset(c *gin.Context, info *relaycommon.RelayInfo, endpoint string) {
	tokenName := c.GetString("token_name")
	other := map[string]any{
		"seedance_asset_endpoint": endpoint,
	}

	logContent := fmt.Sprintf("Seedance 素材 API %s（免费）", endpoint)
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId:      info.ChannelId,
		ModelName:      info.OriginModelName,
		TokenName:      tokenName,
		Quota:          0,
		Content:        logContent,
		TokenId:        info.TokenId,
		UseTimeSeconds: int(time.Since(info.StartTime).Seconds()),
		Group:          info.UsingGroup,
		Other:          other,
	})

	// 请求计数（quota=0 也记录）
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, 0)
}
