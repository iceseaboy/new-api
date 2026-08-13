package submodel

import (
	"testing"

	"github.com/QuantumNous/opclink/model"
	relaycommon "github.com/QuantumNous/opclink/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToH3Request(t *testing.T) {
	t.Run("text to video defaults", func(t *testing.T) {
		req, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{
			Model:  "MiniMax-H3",
			Prompt: "a cat running",
		})
		require.NoError(t, err)
		assert.Equal(t, "MiniMax-H3", req.Model)
		assert.Equal(t, "768P", req.Resolution)
		assert.Equal(t, 5, req.Duration)
		assert.Equal(t, "16:9", req.Ratio)
		require.Len(t, req.Content, 1)
		assert.Equal(t, "text", req.Content[0].Type)
	})

	t.Run("image to video first frame no forced ratio", func(t *testing.T) {
		req, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{
			Model:  "MiniMax-H3",
			Prompt: "wave goodbye",
			Image:  "https://example.com/a.jpg",
			Size:   "2k",
			Seconds: "8",
		})
		require.NoError(t, err)
		assert.Equal(t, "2K", req.Resolution)
		assert.Equal(t, 8, req.Duration)
		assert.Empty(t, req.Ratio)
		require.Len(t, req.Content, 2)
		assert.Equal(t, "first_frame", req.Content[1].Role)
	})

	t.Run("multiple images become reference images", func(t *testing.T) {
		req, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{
			Model:  "MiniMax-H3",
			Prompt: "combine these",
			Images: []string{"https://e.com/1.jpg", "https://e.com/2.jpg"},
		})
		require.NoError(t, err)
		require.Len(t, req.Content, 3)
		assert.Equal(t, "reference_image", req.Content[1].Role)
		assert.Equal(t, "reference_image", req.Content[2].Role)
	})

	t.Run("metadata overrides", func(t *testing.T) {
		req, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{
			Model:  "MiniMax-H3",
			Prompt: "a train",
			Metadata: map[string]interface{}{
				"resolution": "2K",
				"duration":   10,
				"ratio":      "9:16",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "2K", req.Resolution)
		assert.Equal(t, 10, req.Duration)
		assert.Equal(t, "9:16", req.Ratio)
	})

	t.Run("invalid resolution rejected", func(t *testing.T) {
		_, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{Model: "MiniMax-H3", Prompt: "x", Size: "1080P"})
		assert.Error(t, err)
	})

	t.Run("duration out of range rejected", func(t *testing.T) {
		_, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{Model: "MiniMax-H3", Prompt: "x", Duration: 3})
		assert.Error(t, err)
		_, err = convertToH3Request(nil, relaycommon.TaskSubmitReq{Model: "MiniMax-H3", Prompt: "x", Duration: 16})
		assert.Error(t, err)
	})

	t.Run("missing prompt rejected", func(t *testing.T) {
		_, err := convertToH3Request(nil, relaycommon.TaskSubmitReq{Model: "MiniMax-H3", Image: "https://e.com/a.jpg"})
		assert.Error(t, err)
	})
}

func TestParseTaskResultStatusMapping(t *testing.T) {
	a := &TaskAdaptor{}
	cases := []struct {
		body       string
		wantStatus model.TaskStatus
		wantURL    string
		wantReason string
	}{
		{`{"task":{"id":"1","status":"queued"}}`, model.TaskStatusQueued, "", ""},
		{`{"task":{"id":"1","status":"running","progress":{"percent":40}}}`, model.TaskStatusInProgress, "", ""},
		{`{"task":{"id":"1","status":"succeeded","content":{"url":"https://v.mp4"}}}`, model.TaskStatusSuccess, "https://v.mp4", ""},
		{`{"task":{"id":"1","status":"failed","error":{"message":"boom"}}}`, model.TaskStatusFailure, "", "boom"},
		{`{"task":{"id":"1","status":"cancelled"}}`, model.TaskStatusFailure, "", "task cancelled"},
	}
	for _, tc := range cases {
		info, err := a.ParseTaskResult([]byte(tc.body))
		require.NoError(t, err)
		assert.Equal(t, string(tc.wantStatus), info.Status)
		assert.Equal(t, tc.wantURL, info.Url)
		assert.Equal(t, tc.wantReason, info.Reason)
	}
}

func TestAdjustBillingOnCompleteUsesTotalSecondsAndResolution(t *testing.T) {
	a := &TaskAdaptor{}
	task := &model.Task{
		Data: []byte(`{"task":{"id":"1","status":"succeeded","resolution":"2K","usage":{"total_seconds":10,"output_seconds":8,"input_seconds":2}}}`),
	}
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice: 0.08,
		GroupRatio: 1,
	}
	quota := a.AdjustBillingOnComplete(task, nil)
	// 0.08 * 500000 * 1 * (0.13/0.08) * 10 = 650000
	assert.Equal(t, 650000, quota)

	task768 := &model.Task{
		Data: []byte(`{"task":{"id":"1","status":"succeeded","resolution":"768P","usage":{"total_seconds":5}}}`),
	}
	task768.PrivateData.BillingContext = &model.TaskBillingContext{ModelPrice: 0.08, GroupRatio: 0.5}
	// 0.08 * 500000 * 0.5 * 1 * 5 = 100000
	assert.Equal(t, 100000, a.AdjustBillingOnComplete(task768, nil))

	noUsage := &model.Task{Data: []byte(`{"task":{"id":"1","status":"succeeded"}}`)}
	noUsage.PrivateData.BillingContext = &model.TaskBillingContext{ModelPrice: 0.08, GroupRatio: 1}
	assert.Equal(t, 0, a.AdjustBillingOnComplete(noUsage, nil))
}
