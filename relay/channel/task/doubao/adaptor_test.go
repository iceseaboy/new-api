package doubao

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func contentItem(typ, role string) map[string]interface{} {
	m := map[string]interface{}{"type": typ}
	if role != "" {
		m["role"] = role
	}
	return m
}

func TestInferActionFromRequest(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{
			name: "纯文本 → 文生视频",
			req:  relaycommon.TaskSubmitReq{Prompt: "一只猫"},
			want: constant.TaskActionTextGenerate,
		},
		{
			name: "旧式单图 → 图生视频",
			req:  relaycommon.TaskSubmitReq{Images: []string{"https://x/a.png"}},
			want: constant.TaskActionGenerate,
		},
		{
			name: "metadata 首帧 → 图生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("image_url", "first_frame")},
			}},
			want: constant.TaskActionGenerate,
		},
		{
			name: "metadata 图无 role → 图生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("image_url", "")},
			}},
			want: constant.TaskActionGenerate,
		},
		{
			name: "首帧 + 尾帧 → 首尾生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					contentItem("image_url", "first_frame"),
					contentItem("image_url", "last_frame"),
				},
			}},
			want: constant.TaskActionFirstTailGenerate,
		},
		{
			name: "参考图 → 参照生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("image_url", "reference_image")},
			}},
			want: constant.TaskActionReferenceGenerate,
		},
		{
			name: "参考视频 → 参照生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("video_url", "reference_video")},
			}},
			want: constant.TaskActionReferenceGenerate,
		},
		{
			name: "图 + 参考音频 → 参照生视频（参考优先于首帧）",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					contentItem("image_url", "first_frame"),
					contentItem("audio_url", "reference_audio"),
				},
			}},
			want: constant.TaskActionReferenceGenerate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferActionFromRequest(&tc.req); got != tc.want {
				t.Fatalf("inferActionFromRequest = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetVideoBillingRatio(t *testing.T) {
	cases := []struct {
		name       string
		model      string
		resolution string
		hasVideo   bool
		wantRatio  float64
		wantOK     bool
	}{
		{"2.0 默认档", "doubao-seedance-2-0-260128", "", false, 1.0, true},
		{"2.0 720p 含视频", "doubao-seedance-2-0-260128", "720p", true, 28.0 / 46, true},
		{"2.0 1080p", "doubao-seedance-2-0-260128", "1080p", false, 51.0 / 46, true},
		{"2.0 4k", "doubao-seedance-2-0-260128", "4k", false, 26.0 / 46, true},
		{"2.0 4k 含视频", "doubao-seedance-2-0-260128", "4k", true, 16.0 / 46, true},
		{"2.0 大写 4K 归一化", "doubao-seedance-2-0-260128", "4K", false, 26.0 / 46, true},
		{"2.0 大写 1080P 归一化", "doubao-seedance-2-0-260128", "1080P", true, 31.0 / 46, true},
		{"zlhub 别名同价", "doubao-seedance-2.0", "4k", true, 16.0 / 46, true},
		{"fast 不支持 4k 按基准", "doubao-seedance-2-0-fast-260128", "4k", false, 1.0, true},
		{"fast 含视频", "doubao-seedance-2.0-fast", "720p", true, 22.0 / 37, true},
		{"未配置模型", "doubao-seedance-1-0-lite-t2v", "1080p", false, 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := GetVideoBillingRatio(tc.model, tc.resolution, tc.hasVideo)
			if ok != tc.wantOK || got != tc.wantRatio {
				t.Fatalf("GetVideoBillingRatio(%q,%q,%v) = (%v,%v), want (%v,%v)",
					tc.model, tc.resolution, tc.hasVideo, got, ok, tc.wantRatio, tc.wantOK)
			}
		})
	}
}
