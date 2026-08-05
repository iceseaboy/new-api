package kling

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
)

// ============================
// Request / Response structures
// ============================

type TrajectoryPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type DynamicMask struct {
	Mask         string            `json:"mask,omitempty"`
	Trajectories []TrajectoryPoint `json:"trajectories,omitempty"`
}

type CameraConfig struct {
	Horizontal float64 `json:"horizontal,omitempty"`
	Vertical   float64 `json:"vertical,omitempty"`
	Pan        float64 `json:"pan,omitempty"`
	Tilt       float64 `json:"tilt,omitempty"`
	Roll       float64 `json:"roll,omitempty"`
	Zoom       float64 `json:"zoom,omitempty"`
}

type CameraControl struct {
	Type   string        `json:"type,omitempty"`
	Config *CameraConfig `json:"config,omitempty"`
}

type requestPayload struct {
	Prompt         string         `json:"prompt,omitempty"`
	Image          string         `json:"image,omitempty"`
	ImageTail      string         `json:"image_tail,omitempty"`
	NegativePrompt string         `json:"negative_prompt,omitempty"`
	Mode           string         `json:"mode,omitempty"`
	Duration       string         `json:"duration,omitempty"`
	AspectRatio    string         `json:"aspect_ratio,omitempty"`
	ModelName      string         `json:"model_name,omitempty"`
	Model          string         `json:"model,omitempty"` // Compatible with upstreams that only recognize "model"
	CfgScale       float64        `json:"cfg_scale,omitempty"`
	StaticMask     string         `json:"static_mask,omitempty"`
	DynamicMasks   []DynamicMask  `json:"dynamic_masks,omitempty"`
	CameraControl  *CameraControl `json:"camera_control,omitempty"`
	CallbackUrl    string         `json:"callback_url,omitempty"`
	ExternalTaskId string         `json:"external_task_id,omitempty"`
}

type responsePayload struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	TaskId    string `json:"task_id"`
	RequestId string `json:"request_id"`
	Data      struct {
		TaskId        string `json:"task_id"`
		TaskStatus    string `json:"task_status"`
		TaskStatusMsg string `json:"task_status_msg"`
		TaskInfo      struct {
			ExternalTaskId string `json:"external_task_id"`
		} `json:"task_info"`
		WatermarkInfo struct {
			Enabled bool `json:"enabled"`
		} `json:"watermark_info"`
		TaskResult struct {
			Videos []struct {
				Id           string `json:"id"`
				Url          string `json:"url"`
				WatermarkUrl string `json:"watermark_url"`
				Duration     string `json:"duration"`
			} `json:"videos"`
			Images []struct {
				Index        int    `json:"index"`
				Url          string `json:"url"`
				WatermarkUrl string `json:"watermark_url"`
			} `json:"images"`
		} `json:"task_result"`
		CreatedAt          int64  `json:"created_at"`
		UpdatedAt          int64  `json:"updated_at"`
		FinalUnitDeduction string `json:"final_unit_deduction"`
	} `json:"data"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey

	// apiKey format: "access_key|secret_key"
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	// Use the standard validation method for TaskSubmitReq
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	path := lo.Ternary(info.Action == constant.TaskActionGenerate, "/v1/videos/image2video", "/v1/videos/text2video")

	if isNewAPIRelay(info.ApiKey) && !hasCustomPathPrefix(a.baseURL) {
		return fmt.Sprintf("%s/kling%s", a.baseURL, path), nil
	}

	return fmt.Sprintf("%s%s", a.baseURL, path), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	token, err := a.createJWTToken()
	if err != nil {
		return fmt.Errorf("failed to create JWT token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")
	return nil
}

// EstimateBilling 按「输出秒数 × 模式(分辨率)」提供计费乘数。
// 基准价 ModelPrice 定义为 std(720P) 每秒单价；pro(1080P)/4k 按 1.666667 倍（如 $0.07/s ÷ $0.042/s）。
// 下游 kling 原生请求体经 KlingRequestConvert 整体存于 metadata，故从 metadata 取 duration/mode。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	var meta struct {
		Mode     string `json:"mode"`
		Duration string `json:"duration"`
	}
	_ = taskcommon.UnmarshalMetadata(req.Metadata, &meta)
	seconds, _ := strconv.Atoi(meta.Duration)
	if seconds <= 0 {
		seconds = taskcommon.DefaultInt(req.Duration, 5)
	}
	ratios := map[string]float64{"seconds": float64(seconds)}
	if meta.Mode == "pro" || meta.Mode == "4k" {
		ratios["mode"] = 1.666667
	}
	return ratios
}

// BuildRequestBody converts request into Kling specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, err
	}
	if body.Image == "" && body.ImageTail == "" {
		c.Set("action", constant.TaskActionTextGenerate)
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	if action := c.GetString("action"); action != "" {
		info.Action = action
	}
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	var kResp responsePayload
	err = common.Unmarshal(responseBody, &kResp)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}
	if kResp.Code != 0 {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", kResp.Message), "task_failed", http.StatusBadRequest)
		return
	}
	if kResp.Data.TaskId == "" {
		// new-api 中继上游的提交响应是 OpenAIVideo 格式：{"task_id":"task_...","id":"task_...", ...}
		var relaySubmit struct {
			TaskID string `json:"task_id"`
			ID     string `json:"id"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if uerr := common.Unmarshal(responseBody, &relaySubmit); uerr == nil {
			if relaySubmit.Error != nil && relaySubmit.Error.Message != "" {
				taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", relaySubmit.Error.Message), "task_failed", http.StatusBadRequest)
				return
			}
			if relaySubmit.TaskID == "" {
				relaySubmit.TaskID = relaySubmit.ID
			}
			kResp.Data.TaskId = relaySubmit.TaskID
		}
	}
	// 非 kling 信封的上游错误（如 {"replyHeader":{...}}）会解析出 Code=0 且 task_id 为空，
	// 必须判失败，否则产生永远 NOT_START 的幽灵任务且预扣费不退
	if kResp.Data.TaskId == "" {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("upstream did not return task_id: %s", string(responseBody)), "task_failed", http.StatusBadGateway)
		return
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return kResp.Data.TaskId, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	action, ok := body["action"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid action")
	}
	path := lo.Ternary(action == constant.TaskActionGenerate, "/v1/videos/image2video", "/v1/videos/text2video")
	url := fmt.Sprintf("%s%s/%s", baseUrl, path, taskID)
	if isNewAPIRelay(key) && !hasCustomPathPrefix(baseUrl) {
		url = fmt.Sprintf("%s/kling%s/%s", baseUrl, path, taskID)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	token, err := a.createJWTTokenWithKey(key)
	if err != nil {
		token = key
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "kling-sdk/1.0")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"kling-v1", "kling-v1-6", "kling-v2-master"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "kling"
}

// ============================
// helpers
// ============================

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	r := requestPayload{
		Prompt:         req.Prompt,
		Image:          req.Image,
		Mode:           taskcommon.DefaultString(req.Mode, "std"),
		Duration:       fmt.Sprintf("%d", taskcommon.DefaultInt(req.Duration, 5)),
		AspectRatio:    a.getAspectRatio(req.Size),
		ModelName:      info.UpstreamModelName,
		Model:          info.UpstreamModelName,
		// cfg_scale 不设默认值：kling-v2.x 不支持该参数（传了直接报错），
		// v1 系上游缺省即为 0.5；用户显式传入的仍经 metadata 透传
		StaticMask:     "",
		DynamicMasks:   []DynamicMask{},
		CameraControl:  nil,
		CallbackUrl:    "",
		ExternalTaskId: "",
	}
	if r.ModelName == "" {
		r.ModelName = "kling-v1"
		r.Model = "kling-v1"
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}
	return &r, nil
}

func (a *TaskAdaptor) getAspectRatio(size string) string {
	switch size {
	case "1024x1024", "512x512":
		return "1:1"
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	default:
		return "1:1"
	}
}

// ============================
// JWT helpers
// ============================

func (a *TaskAdaptor) createJWTToken() (string, error) {
	return a.createJWTTokenWithKey(a.apiKey)
}

func (a *TaskAdaptor) createJWTTokenWithKey(apiKey string) (string, error) {
	if isNewAPIRelay(apiKey) {
		return apiKey, nil // new api relay
	}
	keyParts := strings.Split(apiKey, "|")
	if len(keyParts) != 2 {
		return "", errors.New("invalid api_key, required format is accessKey|secretKey")
	}
	accessKey := strings.TrimSpace(keyParts[0])
	if len(keyParts) == 1 {
		return accessKey, nil
	}
	secretKey := strings.TrimSpace(keyParts[1])
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iss": accessKey,
		"exp": now + 1800, // 30 minutes
		"nbf": now - 5,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = "JWT"
	return token.SignedString([]byte(secretKey))
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	taskInfo := &relaycommon.TaskInfo{}
	resPayload := responsePayload{}
	err := common.Unmarshal(respBody, &resPayload)
	if err != nil {
		// new-api 中继上游的查询响应是 TaskDto 信封（code 为字符串），回退解析
		if relayInfo, relayErr := parseNewAPIRelayTaskResult(respBody); relayErr == nil {
			return relayInfo, nil
		}
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}
	taskInfo.Code = resPayload.Code
	taskInfo.TaskID = resPayload.Data.TaskId
	taskInfo.Reason = resPayload.Data.TaskStatusMsg
	//任务状态，枚举值：submitted（已提交）、processing（处理中）、succeed（成功）、failed（失败）
	status := resPayload.Data.TaskStatus
	switch status {
	case "submitted":
		taskInfo.Status = model.TaskStatusSubmitted
	case "processing":
		taskInfo.Status = model.TaskStatusInProgress
	case "succeed":
		taskInfo.Status = model.TaskStatusSuccess
		if videos := resPayload.Data.TaskResult.Videos; len(videos) > 0 {
			video := videos[0]
			taskInfo.Url = video.Url
		}
		if tokens, err := strconv.ParseFloat(resPayload.Data.FinalUnitDeduction, 64); err == nil {
			// 上游返回的扣费数值，饱和转换防止超大数值回绕成负数
			rounded := common.QuotaFromFloat(math.Ceil(tokens))
			if rounded > 0 {
				taskInfo.CompletionTokens = rounded
				taskInfo.TotalTokens = rounded
			}
		}
	case "failed":
		taskInfo.Status = model.TaskStatusFailure
	default:
		return nil, fmt.Errorf("unknown task status: %s", status)
	}
	return taskInfo, nil
}

func isNewAPIRelay(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-")
}

// parseNewAPIRelayTaskResult 解析 new-api 中继上游的任务查询响应（TaskDto 信封）：
// {"code":"success","data":{"status":"SUCCESS","progress":"100%","fail_reason":"","data":{...kling 原生响应...}}}
// 状态取外层 TaskDto.status；视频 URL 从内层 kling 原生数据提取。
func parseNewAPIRelayTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var relayResp struct {
		Data struct {
			Status     string          `json:"status"`
			Progress   string          `json:"progress"`
			FailReason string          `json:"fail_reason"`
			Data       json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := common.Unmarshal(respBody, &relayResp); err != nil {
		return nil, err
	}
	status := model.TaskStatus(relayResp.Data.Status)
	switch status {
	case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued,
		model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure:
	default:
		return nil, fmt.Errorf("unknown relay task status: %q", relayResp.Data.Status)
	}
	taskInfo := &relaycommon.TaskInfo{
		Status:   string(status),
		Progress: relayResp.Data.Progress,
		Reason:   relayResp.Data.FailReason,
	}
	if status == model.TaskStatusSuccess {
		taskInfo.Url = extractRelayVideoURL(relayResp.Data.Data)
	}
	return taskInfo, nil
}

// extractRelayVideoURL 逐层拆开中继信封（可能多级 new-api 串联，每级包一层 data），
// 直到最内层 kling 原生数据中的 task_result.videos[0].url。
func extractRelayVideoURL(raw json.RawMessage) string {
	for depth := 0; depth < 6 && len(raw) > 0; depth++ {
		var probe struct {
			TaskResult struct {
				Videos []struct {
					Url string `json:"url"`
				} `json:"videos"`
			} `json:"task_result"`
			Data json.RawMessage `json:"data"`
		}
		if err := common.Unmarshal(raw, &probe); err != nil {
			return ""
		}
		if videos := probe.TaskResult.Videos; len(videos) > 0 {
			return videos[0].Url
		}
		raw = probe.Data
	}
	return ""
}

// hasCustomPathPrefix 判断渠道 base_url 是否带路径前缀（如 https://api.sinmo.top/openapi）。
// 此类上游在该前缀下直接暴露 kling 原生路径，不能再插入 new-api 中继的 /kling 段。
func hasCustomPathPrefix(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return u.Path != "" && u.Path != "/"
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var klingResp responsePayload
	if err := common.Unmarshal(originTask.Data, &klingResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal kling task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = klingResp.Data.CreatedAt
	openAIVideo.CompletedAt = klingResp.Data.UpdatedAt

	if len(klingResp.Data.TaskResult.Videos) > 0 {
		video := klingResp.Data.TaskResult.Videos[0]
		if video.Url != "" {
			openAIVideo.SetMetadata("url", video.Url)
		}
		if video.Duration != "" {
			openAIVideo.Seconds = video.Duration
		}
	}

	if klingResp.Code != 0 && klingResp.Message != "" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: klingResp.Message,
			Code:    fmt.Sprintf("%d", klingResp.Code),
		}
	}

	// https://app.klingai.com/cn/dev/document-api/apiReference/model/textToVideo
	if data := klingResp.Data; data.TaskStatus == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: data.TaskStatusMsg,
		}
	}
	return common.Marshal(openAIVideo)
}
