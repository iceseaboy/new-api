package controller

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/opclink/common"
	"github.com/QuantumNous/opclink/constant"
	"github.com/QuantumNous/opclink/logger"
	"github.com/QuantumNous/opclink/model"
	"github.com/QuantumNous/opclink/relay"
	relaycommon "github.com/QuantumNous/opclink/relay/common"
	"github.com/QuantumNous/opclink/service"
	"github.com/QuantumNous/opclink/relaykit/types"

	"github.com/gin-gonic/gin"
)

// RelaySeedanceAsset 处理 Seedance 素材 API（CreateAssetGroup / CreateAsset / GetAsset）。
// 素材接口免费且与具体生成模型无关：不走 ModelPriceHelper / 预扣费，
// 仅复用 Distribute 选路 + 重试循环（与 Relay 主流程同构）。
func RelaySeedanceAsset(c *gin.Context) {
	requestId := c.GetString(common.RequestIdKey)

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		opclinkError := types.NewError(fmt.Errorf("生成 relay 信息失败: %w", err), types.ErrorCodeGenRelayInfoFailed)
		logger.LogError(c, fmt.Sprintf("relay error: %s", opclinkError.Error()))
		opclinkError.SetMessage(common.MessageWithRequestId(opclinkError.Error(), requestId))
		c.JSON(opclinkError.StatusCode, gin.H{
			"error": opclinkError.ToOpenAIError(),
		})
		return
	}

	var opclinkError *types.OPCLinkError

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			opclinkError = channelErr
			break
		}

		addUsedChannel(c, channel.Id)

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			opclinkError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		opclinkError = relay.SeedanceAssetHelper(c, relayInfo)

		if opclinkError == nil {
			// 成功（响应已写回客户端）
			return
		}

		// 渠道错误处理
		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), opclinkError)

		if !shouldRetry(c, opclinkError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	// 所有重试用完仍失败
	if opclinkError != nil {
		useChannel := c.GetStringSlice("use_channel")
		if len(useChannel) > 1 {
			logger.LogInfo(c, fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]")))
		}
		logger.LogError(c, fmt.Sprintf("relay error: %s", opclinkError.Error()))
		opclinkError.SetMessage(common.MessageWithRequestId(opclinkError.Error(), requestId))
		c.JSON(opclinkError.StatusCode, gin.H{
			"error": opclinkError.ToOpenAIError(),
		})
	}
}

// SeedanceAssetList 本地素材列表（仅查本地归属记录，不透传上游）。
// Distribute 中间件已通过 seedance-asset 虚拟模型完成令牌模型权限校验并选好
// channel，channelId 从 context 取，列表按 userId + channelId 隔离。
func SeedanceAssetList(c *gin.Context) {
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	channelId := c.GetInt("channel_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	assets, total, err := model.GetVendorAssets(userId, channelId, page, pageSize)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("查询本地素材列表失败: %s", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": assets, "total": total, "page": page, "page_size": pageSize})
}
