package ali

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/opclink/common"
	relaycommon "github.com/QuantumNous/opclink/relay/common"
	relayhelper "github.com/QuantumNous/opclink/relay/helper"
	"github.com/QuantumNous/opclink/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestFiltersThinkingBudgetByUpstreamModel(t *testing.T) {
	tests := []struct {
		name          string
		requestModel  string
		upstreamModel string
		budget        string
		wantBudget    bool
		wantValue     int64
	}{
		{
			name:          "qwen",
			requestModel:  "qwen-plus",
			upstreamModel: "qwen-plus",
			budget:        "128",
			wantBudget:    true,
			wantValue:     128,
		},
		{
			name:          "qwq explicit zero",
			requestModel:  "qwq-32b",
			upstreamModel: "qwq-32b",
			budget:        "0",
			wantBudget:    true,
			wantValue:     0,
		},
		{
			name:          "unsupported upstream overrides qwen request",
			requestModel:  "qwen-plus",
			upstreamModel: "deepseek-r1",
			budget:        "128",
			wantBudget:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{
				Model:          tt.requestModel,
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: json.RawMessage(tt.budget),
			}
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: tt.upstreamModel,
				},
			}

			convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
			require.NoError(t, err)
			converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)

			if tt.wantBudget {
				assert.Equal(t, tt.budget, string(converted.ThinkingBudget))
			} else {
				assert.Nil(t, converted.ThinkingBudget)
			}

			encoded, err := common.Marshal(converted)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.Equal(t, tt.wantBudget, value.Exists())
			if tt.wantBudget {
				assert.Equal(t, tt.wantValue, value.Int())
			}
		})
	}
}

func TestConvertOpenAIRequestPreservesExplicitZeroForMappedQwenModel(t *testing.T) {
	const (
		clientModel   = "customer-model"
		upstreamModel = "Qwen/Qwen3-235B-A22B-Thinking-2507"
	)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"customer-model":"Qwen/Qwen3-235B-A22B-Thinking-2507"}`)

	request := &dto.GeneralOpenAIRequest{
		Model:          clientModel,
		EnableThinking: json.RawMessage(`true`),
		ThinkingBudget: json.RawMessage(`0`),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: clientModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: clientModel,
		},
	}

	err := relayhelper.ModelMappedHelper(c, info, request)
	require.NoError(t, err)
	assert.True(t, info.IsModelMapped)
	assert.Equal(t, upstreamModel, info.UpstreamModelName)
	assert.Equal(t, upstreamModel, request.Model)

	convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, json.RawMessage(`0`), converted.ThinkingBudget)

	encoded, err := common.Marshal(converted)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(0), value.Int())
}

func TestAliImageResolutionRatio(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		params    AliImageParameters
		wantTier  string
		wantRatio float64
		wantOK    bool
	}{
		{"q3-fast 默认1K", "vidu/viduq3-fast_reference2image", AliImageParameters{}, "1K", 1.0, true},
		{"q3-fast resolution=2K", "vidu/viduq3-fast_reference2image", AliImageParameters{Resolution: "2k"}, "2K", 0.78125 / 0.46875, true},
		{"q3-fast size推断4K", "vidu/viduq3-fast_reference2image", AliImageParameters{Size: "4096*2160"}, "4K", 1.09375 / 0.46875, true},
		{"q2-pro 2K同1K价", "vidu/viduq2-pro_reference2image", AliImageParameters{Size: "2048*2048"}, "2K", 1.0, true},
		{"q2-pro 4K", "vidu/viduq2-pro_reference2image", AliImageParameters{Resolution: "4K"}, "4K", 1.71875 / 0.9375, true},
		{"q2-fast 不支持档按基准", "vidu/viduq2-fast_reference2image", AliImageParameters{Resolution: "4K"}, "4K", 1.0, true},
		{"qwen-image-pro 默认1K", "qwen-image-3.0-pro", AliImageParameters{}, "1K", 1.0, true},
		{"qwen-image-pro resolution=2K", "qwen-image-3.0-pro", AliImageParameters{Resolution: "2K"}, "2K", 2.0, true},
		{"qwen-image-pro size推断2K", "qwen-image-3.0-pro", AliImageParameters{Size: "2048*2048"}, "2K", 2.0, true},
		{"qwen-image 普通版无档位表", "qwen-image-3.0", AliImageParameters{Size: "2048*2048"}, "", 0, false},
		{"非配价模型", "wanx-v1", AliImageParameters{}, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, ratio, ok := aliImageResolutionRatio(tc.model, &tc.params)
			if ok != tc.wantOK || tier != tc.wantTier {
				t.Fatalf("tier=%q ok=%v, want %q %v", tier, ok, tc.wantTier, tc.wantOK)
			}
			if ok {
				if d := ratio - tc.wantRatio; d > 1e-9 || d < -1e-9 {
					t.Fatalf("ratio=%v want %v", ratio, tc.wantRatio)
				}
			}
		})
	}
}
