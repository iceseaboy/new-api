package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserModelRatioMatching(t *testing.T) {
	require.NoError(t, UpdateUserModelRatioByJSONString(`{
		"28": {
			"glm-5.2*": 0.55,
			"deepseek*": 0.6,
			"qwen*": 0.65,
			"qwen-image-3.0": 0.45,
			"qwen-image-*": 0.5,
			"happyhorse*": 0.5
		}
	}`))
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRatioByJSONString(`{}`))
	})

	tests := []struct {
		name      string
		userId    int
		model     string
		wantRatio float64
		wantOk    bool
	}{
		{"wildcard matches family member", 28, "glm-5.2-fast-preview", 0.55, true},
		{"wildcard matches bare prefix", 28, "glm-5.2", 0.55, true},
		{"no match for sibling version", 28, "glm-5.1", 1, false},
		{"case sensitive", 28, "GLM-5.2", 1, false},
		{"longest prefix beats shorter", 28, "qwen-image-3.0-pro", 0.5, true},
		{"exact beats every wildcard", 28, "qwen-image-3.0", 0.45, true},
		{"short prefix still covers others", 28, "qwen3.8-max", 0.65, true},
		{"video family", 28, "happyhorse-1.1-t2v", 0.5, true},
		{"other user unaffected", 29, "glm-5.2", 1, false},
		{"invalid user id", 0, "glm-5.2", 1, false},
		{"empty model", 28, "", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok := GetUserModelRatio(tt.userId, tt.model)
			assert.Equal(t, tt.wantOk, ok)
			assert.InDelta(t, tt.wantRatio, ratio, 1e-9)
		})
	}
}

func TestUpdateUserModelRatioValidation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"valid", `{"28": {"glm-5.2*": 0.55}}`, false},
		{"empty object", `{}`, false},
		{"surcharge within cap", `{"28": {"m": 2}}`, false},
		{"non numeric user key", `{"guohang": {"m": 0.5}}`, true},
		{"zero user id", `{"0": {"m": 0.5}}`, true},
		{"negative user id", `{"-1": {"m": 0.5}}`, true},
		{"zero ratio", `{"28": {"m": 0}}`, true},
		{"negative ratio", `{"28": {"m": -0.5}}`, true},
		{"ratio above cap", `{"28": {"m": 11}}`, true},
		{"bare star pattern", `{"28": {"*": 0.5}}`, true},
		{"mid string star", `{"28": {"a*b": 0.5}}`, true},
		{"empty pattern", `{"28": {"": 0.5}}`, true},
		{"not an object", `[1,2]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UpdateUserModelRatioByJSONString(tt.json)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
	require.NoError(t, UpdateUserModelRatioByJSONString(`{}`))
}
