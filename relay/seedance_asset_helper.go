package relay

import (
	"bytes"
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
	targetURL, err := buildSeedanceAssetURL(info, endpoint)
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
	// zlhub 资产管理 API 用 X-Access-Token 鉴权（配 {"X-Access-Token":"{api_key}"}），
	// 并要求每次请求带唯一的 32 位十六进制 X-Track-Id 便于排障。
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Track-Id", common.GetUUID())
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

	// ---- 上游错误响应（HTTP 4xx/5xx → {Code, Message, TrackId}） ----
	// zlhub 用 HTTP 状态码区分成功/失败：成功 200 返回 {ResponseMetadata, Result}，
	// 失败返回顶层 {Code, Message}。这里透出上游真实错误，并按状态码决定是否重试。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, msg := extractSeedanceAssetError(body)
		logger.LogError(c, fmt.Sprintf("Seedance 素材上游错误: endpoint=%s, http=%d, code=%s, msg=%s, body=%s",
			endpoint, resp.StatusCode, code, msg, string(body)))
		detail := msg
		if code != "" {
			if detail != "" {
				detail = code + ": " + detail
			} else {
				detail = code
			}
		}
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		// 5xx（如 502 VolcengineCallFailed）为上游瞬时异常，可重试换渠道；
		// 4xx（参数/权限/限流）为确定性错误，不重试，原样透出。
		if resp.StatusCode >= 500 {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("上游服务异常: %s", detail),
				types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
		}
		errCode := types.ErrorCodeInvalidRequest
		if resp.StatusCode == http.StatusForbidden {
			errCode = types.ErrorCodeAccessDenied
		}
		return types.NewErrorWithStatusCode(
			fmt.Errorf("%s", detail), errCode, resp.StatusCode, types.ErrOptionWithSkipRetry())
	}
	logger.LogInfo(c, fmt.Sprintf("Seedance 上游响应 HTTP %d, endpoint=%s, body=%s", resp.StatusCode, endpoint, string(body)))

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

	// ---- DB 成功 → 翻译成下游统一契约 {state,data,error} 返回客户端 ----
	// 网关对下游屏蔽上游(zlhub)的 {ResponseMetadata,Result} 外壳，统一对外暴露
	// {"state":1,"data":<Result>,"error":null}，与素材管理接口文档一致。
	if apiErr := writeSeedanceDownstreamResponse(c, body); apiErr != nil {
		return apiErr
	}

	// ---- postConsume（免费：quota=0，只记日志和请求计数） ----
	postConsumeSeedanceAsset(c, info, endpoint)

	return nil
}

// ---- URL 映射 ----

// buildSeedanceAssetURL 构建上游请求 URL。
// zlhub 资产管理 API 用查询参数 Action 指定操作：POST {base_url}?Action={endpoint}
// base_url 应配置为完整路径，如 https://asset.zlhub.cn/api/asset-management
func buildSeedanceAssetURL(info *relaycommon.RelayInfo, endpoint string) (string, error) {
	baseURL := info.ChannelBaseUrl
	if baseURL == "" {
		return "", fmt.Errorf("Seedance asset channel base URL 未配置")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return baseURL + "?Action=" + endpoint, nil
}

// ---- 下游响应翻译 ----

// writeSeedanceDownstreamResponse 把上游(zlhub) {ResponseMetadata,Result} 翻译成
// 下游统一契约 {"state":1,"data":<Result>,"error":null} 返回客户端，
// 让客户端可按素材管理接口文档(state/data/error)对接，与上游外壳解耦。
func writeSeedanceDownstreamResponse(c *gin.Context, body []byte) *types.NewAPIError {
	var envelope dto.SeedanceAssetEnvelope[any]
	if common.Unmarshal(body, &envelope) != nil || envelope.Result == nil {
		logger.LogError(c, fmt.Sprintf("素材响应翻译失败: 无法解析 Result, upstream_body=%s", string(body)))
		return types.NewError(fmt.Errorf("上游响应格式异常"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	c.JSON(http.StatusOK, gin.H{
		"state": 1,
		"data":  envelope.Result,
		"error": nil,
	})
	return nil
}

// ---- Ownership 处理 ----

func handleCreateAssetGroup(c *gin.Context, userId, tokenId, channelId int, respBody []byte) *types.NewAPIError {
	var outer dto.SeedanceAssetEnvelope[dto.SeedanceCreateAssetGroupResult]
	if common.Unmarshal(respBody, &outer) != nil || outer.Result.Id == "" {
		logger.LogError(c, fmt.Sprintf("CreateAssetGroup: 无法从上游响应解析 Id, upstream_body=%s", string(respBody)))
		return types.NewError(fmt.Errorf("上游响应格式异常"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}

	group := &model.VendorAssetGroup{
		UserId:       userId,
		TokenId:      tokenId,
		ChannelId:    channelId,
		AssetGroupId: outer.Result.Id,
		CreatedAt:    time.Now().Unix(),
	}
	alreadyExists, err := group.Insert()
	if err != nil {
		logger.LogError(c, fmt.Sprintf("写入素材组 ownership 失败: group_id=%s, user_id=%d, err=%s",
			outer.Result.Id, userId, err.Error()))
		return types.NewError(fmt.Errorf("素材组归属记录失败，请联系管理员"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if alreadyExists {
		logger.LogWarn(c, fmt.Sprintf("素材组已存在（幂等）: group_id=%s, user_id=%d", outer.Result.Id, userId))
	}

	return nil
}

func handleCreateAsset(c *gin.Context, userId, tokenId, channelId int, respBody []byte, groupId, assetType string) *types.NewAPIError {
	var outer dto.SeedanceAssetEnvelope[dto.SeedanceCreateAssetResult]
	if common.Unmarshal(respBody, &outer) != nil || outer.Result.Id == "" {
		logger.LogError(c, fmt.Sprintf("CreateAsset: 无法从上游响应解析 Id, upstream_body=%s", string(respBody)))
		return types.NewError(fmt.Errorf("上游响应格式异常"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}

	// group 归属已在转发前校验通过，直接入 DB
	asset := &model.VendorAsset{
		UserId:       userId,
		TokenId:      tokenId,
		ChannelId:    channelId,
		AssetId:      outer.Result.Id,
		AssetGroupId: groupId,
		AssetType:    strings.ToLower(assetType),
		CreatedAt:    time.Now().Unix(),
	}
	alreadyExists, err := asset.Insert()
	if err != nil {
		logger.LogError(c, fmt.Sprintf("写入素材 ownership 失败: asset_id=%s, group_id=%s, user_id=%d, err=%s",
			outer.Result.Id, groupId, userId, err.Error()))
		return types.NewError(fmt.Errorf("素材归属记录失败，请联系管理员"),
			types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if alreadyExists {
		logger.LogWarn(c, fmt.Sprintf("素材已存在（幂等）: asset_id=%s, group_id=%s, user_id=%d",
			outer.Result.Id, groupId, userId))
	}

	return nil
}

// ---- 上游错误检测 ----

// extractSeedanceAssetError 从 zlhub 错误响应体解析 {Code, Message}。
// 上游失败时 HTTP 为 4xx/5xx，响应体形如 {"Code":"GroupNotOwned","Message":"...","TrackId":"..."}。
// 解析失败时返回空串，调用方会回退到 HTTP 状态码描述。
func extractSeedanceAssetError(body []byte) (code, message string) {
	var e dto.SeedanceAssetError
	if common.Unmarshal(body, &e) == nil {
		return e.Code, e.Message
	}
	return "", ""
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
