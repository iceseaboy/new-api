package common

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestNormalizeForCompatibility_TopLevelContent(t *testing.T) {
	req := TaskSubmitReq{
		Content: []TaskContentItem{
			{Type: "text", Text: "hello world"},
			{Type: "image_url", Role: "first_frame", ImageURL: &TaskMediaURL{URL: "asset://abc"}},
		},
		Metadata: map[string]interface{}{"resolution": "720p"},
	}
	req.NormalizeForCompatibility()

	if req.Prompt != "hello world" {
		t.Fatalf("prompt should be extracted from content text, got %q", req.Prompt)
	}
	content, ok := req.Metadata["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("metadata.content should be injected with 2 items, got %#v", req.Metadata["content"])
	}
	if req.Metadata["resolution"] != "720p" {
		t.Fatalf("existing metadata fields must be preserved")
	}
	// 注入项应保留 type/role/url
	first, _ := content[1].(map[string]interface{})
	if first["type"] != "image_url" || first["role"] != "first_frame" {
		t.Fatalf("injected content item lost fields: %#v", first)
	}
}

func TestNormalizeForCompatibility_Idempotentish(t *testing.T) {
	// 已有 prompt 与 metadata.content 时不应被覆盖
	req := TaskSubmitReq{
		Prompt:   "explicit prompt",
		Content:  []TaskContentItem{{Type: "text", Text: "content text"}},
		Metadata: map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": "existing"}}},
	}
	req.NormalizeForCompatibility()
	if req.Prompt != "explicit prompt" {
		t.Fatalf("existing prompt must not be overwritten, got %q", req.Prompt)
	}
	content := req.Metadata["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("existing metadata.content must not be overwritten")
	}
}

func TestNormalizeForCompatibility_LegacyImage(t *testing.T) {
	req := TaskSubmitReq{Image: "https://x/a.png", Prompt: "p"}
	req.NormalizeForCompatibility()
	if len(req.Images) != 1 || req.Images[0] != "https://x/a.png" {
		t.Fatalf("legacy Image should map to Images, got %#v", req.Images)
	}
}

func TestNormalizeForCompatibility_NoContent(t *testing.T) {
	req := TaskSubmitReq{Prompt: "p"}
	req.NormalizeForCompatibility()
	if req.Metadata != nil && req.Metadata["content"] != nil {
		t.Fatalf("no content should not create metadata.content")
	}
}

// 顶层视频参数（火山原生 / OpenAI 风格）应被合并进 metadata，供下游适配器读取。
func TestUnmarshalJSON_TopLevelVideoParams(t *testing.T) {
	body := `{
		"model": "doubao-seedance-2-0-260128",
		"content": [{"type":"text","text":"hi"}],
		"resolution": "720p",
		"ratio": "16:9",
		"duration": 15,
		"generate_audio": true,
		"watermark": false
	}`
	var req TaskSubmitReq
	if err := common.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Duration != 15 {
		t.Fatalf("top-level duration should parse to req.Duration=15, got %d", req.Duration)
	}
	want := map[string]interface{}{
		"resolution":     "720p",
		"ratio":          "16:9",
		"generate_audio": true,
		"watermark":      false,
	}
	for k, w := range want {
		if got := req.Metadata[k]; got != w {
			t.Fatalf("metadata[%q] = %#v, want %#v", k, got, w)
		}
	}
	// duration 也应补进 metadata（下游从 metadata 读，而非 req.Duration）
	if got := fmt.Sprint(req.Metadata["duration"]); got != "15" {
		t.Fatalf("metadata[duration] = %v, want 15", req.Metadata["duration"])
	}
}

// metadata 已有的键优先，不被顶层同名键覆盖（向后兼容既有 metadata 写法）。
func TestUnmarshalJSON_MetadataTakesPrecedence(t *testing.T) {
	body := `{
		"model": "m",
		"resolution": "720p",
		"metadata": {"resolution": "1080p"}
	}`
	var req TaskSubmitReq
	if err := common.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Metadata["resolution"] != "1080p" {
		t.Fatalf("metadata should win over top-level, got %#v", req.Metadata["resolution"])
	}
}
