package doubao

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			want: constant.TaskActionTextToVideo,
		},
		{
			name: "旧式单图 → 图生视频",
			req:  relaycommon.TaskSubmitReq{Images: []string{"https://x/a.png"}},
			want: constant.TaskActionImageToVideo,
		},
		{
			name: "metadata 首帧 → 图生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("image_url", "first_frame")},
			}},
			want: constant.TaskActionImageToVideo,
		},
		{
			name: "metadata 图无 role → 图生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("image_url", "")},
			}},
			want: constant.TaskActionImageToVideo,
		},
		{
			name: "首帧 + 尾帧 → 首尾生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					contentItem("image_url", "first_frame"),
					contentItem("image_url", "last_frame"),
				},
			}},
			want: constant.TaskActionFirstTailToVideo,
		},
		{
			name: "参考图 → 参照生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("image_url", "reference_image")},
			}},
			want: constant.TaskActionReferenceToVideo,
		},
		{
			name: "参考视频 → 参照生视频",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{contentItem("video_url", "reference_video")},
			}},
			want: constant.TaskActionReferenceToVideo,
		},
		{
			name: "图 + 参考音频 → 参照生视频（参考优先于首帧）",
			req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
				"content": []interface{}{
					contentItem("image_url", "first_frame"),
					contentItem("audio_url", "reference_audio"),
				},
			}},
			want: constant.TaskActionReferenceToVideo,
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

func TestGetVideoInputRatio(t *testing.T) {
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
			got, ok := GetVideoInputRatio(tc.model, tc.resolution, tc.hasVideo)
			if ok != tc.wantOK || got != tc.wantRatio {
				t.Fatalf("GetVideoInputRatio(%q,%q,%v) = (%v,%v), want (%v,%v)",
					tc.model, tc.resolution, tc.hasVideo, got, ok, tc.wantRatio, tc.wantOK)
			}
		})
	}
}

func TestParseNewAPIRelayTaskResult(t *testing.T) {
	// SUCCESS：内层为火山原生数据（含 URL/usage/分辨率）
	body := []byte(`{"code":"success","data":{"status":"SUCCESS","progress":"100%","fail_reason":"","data":{"id":"cgt-1","status":"succeeded","content":{"video_url":"https://x/v.mp4"},"resolution":"1080p","usage":{"completion_tokens":100,"total_tokens":120}}}}`)
	got, err := parseNewAPIRelayTaskResult(body)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Status != "SUCCESS" || got.Url != "https://x/v.mp4" || got.TotalTokens != 120 || got.Resolution != "1080p" {
		t.Fatalf("bad result: %+v", got)
	}

	// 双层嵌套信封（多级 new-api 串联）
	nested := []byte(`{"code":"success","data":{"status":"SUCCESS","data":{"data":{"content":{"video_url":"https://x/n.mp4"},"resolution":"720p","usage":{"total_tokens":80}}}}}`)
	got, err = parseNewAPIRelayTaskResult(nested)
	if err != nil || got.Url != "https://x/n.mp4" || got.Resolution != "720p" {
		t.Fatalf("nested parse failed: %+v err=%v", got, err)
	}

	// FAILURE 带原因
	failBody := []byte(`{"code":"success","data":{"status":"FAILURE","progress":"100%","fail_reason":"content moderation"}}`)
	got, err = parseNewAPIRelayTaskResult(failBody)
	if err != nil || got.Status != "FAILURE" || got.Reason != "content moderation" {
		t.Fatalf("failure parse failed: %+v err=%v", got, err)
	}

	// 火山原生响应（非信封）应判为不认识 → 回退原生解析
	native := []byte(`{"id":"cgt-2","status":"succeeded","content":{"video_url":"https://x/v.mp4"}}`)
	if _, err = parseNewAPIRelayTaskResult(native); err == nil {
		t.Fatal("native body should not parse as relay envelope")
	}
}

func TestIsNewAPIRelay(t *testing.T) {
	cases := []struct {
		name string
		key  string
		base string
		want bool
	}{
		{"tokease 裸域名 sk-", "sk-abc", "https://tokease.cn", true},
		{"zlhub sk- 但带 /origin 前缀 → 原生", "sk-abc", "https://api.zlhub.cn/origin", false},
		{"火山官方 UUID 密钥", "0af5e219-8305-4b2f", "https://ark.cn-beijing.volces.com", false},
		{"尾斜杠视为无前缀", "sk-abc", "https://tokease.cn/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNewAPIRelay(tc.key, tc.base); got != tc.want {
				t.Fatalf("isNewAPIRelay(%q,%q) = %v, want %v", tc.key, tc.base, got, tc.want)
			}
		})
	}
}

// 中继上游分两类读法：只认顶层字段的与从 metadata 读的。TaskSubmitReq 只有部分
// 生成参数是一等成员（resolution/ratio 等仅存在于 metadata），若不补写顶层，
// 只读顶层的中继上游会拿不到 content 与生成参数，静默退化成纯文生视频且按默认档计费。
func TestHoistMetadataToTopLevelKeepsBothReadPaths(t *testing.T) {
	body := []byte(`{"prompt":"hi","model":"m","duration":4,"metadata":{"resolution":"480p","ratio":"9:16","content":[{"type":"text","text":"hi"}],"duration":4}}`)

	var got map[string]any
	require.NoError(t, common.Unmarshal(hoistMetadataToTopLevel(body), &got))

	assert.Equal(t, "480p", got["resolution"], "顶层应补出 resolution")
	assert.Equal(t, "9:16", got["ratio"], "顶层应补出 ratio")
	assert.NotNil(t, got["content"], "顶层应补出 content")
	assert.NotNil(t, got["metadata"], "metadata 必须原样保留，兼容只读 metadata 的下游")
	assert.EqualValues(t, 4, got["duration"], "顶层已有的键不被 metadata 覆盖")
}

func TestHoistMetadataToTopLevelNoMetadataIsUnchanged(t *testing.T) {
	body := []byte(`{"prompt":"hi","model":"m"}`)
	assert.JSONEq(t, string(body), string(hoistMetadataToTopLevel(body)))
}

// The host stores its own task_id and keeps the upstream one in PrivateData;
// polling must query upstream by the latter or relay/hosted tasks are reported
// as not found and refunded.
func TestFetchTaskQueriesUpstreamTaskID(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	service.InitHttpClient()

	task := &model.Task{TaskID: "task_local", PrivateData: model.TaskPrivateData{UpstreamTaskID: "task_upstream"}}
	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "sk-test", task, "")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Contains(t, gotPath, "task_upstream")
	assert.NotContains(t, gotPath, "task_local")
}
