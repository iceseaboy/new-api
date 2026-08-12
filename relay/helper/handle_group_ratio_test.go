package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/opclink/relay/common"
	"github.com/QuantumNous/opclink/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGroupRatioAppliesUserModelRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, ratio_setting.UpdateUserModelRatioByJSONString(`{"28": {"glm-5.2*": 0.55}}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateUserModelRatioByJSONString(`{}`))
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	discounted := HandleGroupRatio(c, &relaycommon.RelayInfo{
		UserId:          28,
		UserGroup:       "default",
		UsingGroup:      "default",
		OriginModelName: "glm-5.2-fast-preview",
	})
	assert.InDelta(t, 0.55, discounted.GroupRatio, 1e-9)
	assert.True(t, discounted.HasUserModelRatio)
	assert.InDelta(t, 0.55, discounted.UserModelRatio, 1e-9)

	fullPrice := HandleGroupRatio(c, &relaycommon.RelayInfo{
		UserId:          29,
		UserGroup:       "default",
		UsingGroup:      "default",
		OriginModelName: "glm-5.2-fast-preview",
	})
	assert.InDelta(t, 1.0, fullPrice.GroupRatio, 1e-9)
	assert.False(t, fullPrice.HasUserModelRatio)

	otherModel := HandleGroupRatio(c, &relaycommon.RelayInfo{
		UserId:          28,
		UserGroup:       "default",
		UsingGroup:      "default",
		OriginModelName: "kimi-k3",
	})
	assert.InDelta(t, 1.0, otherModel.GroupRatio, 1e-9)
	assert.False(t, otherModel.HasUserModelRatio)
}
