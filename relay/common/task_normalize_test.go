package common

import (
	"testing"
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
