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
