package submodel

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/opclink/common"
	taskdto "github.com/QuantumNous/opclink/dto"
	"github.com/QuantumNous/opclink/logger"
	"github.com/QuantumNous/opclink/model"
	"github.com/QuantumNous/opclink/relay/channel"
	"github.com/QuantumNous/opclink/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/opclink/relay/common"
	"github.com/QuantumNous/opclink/relaykit/dto"
	"github.com/QuantumNous/opclink/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// MiniMax H3（h3.submodel.ai）视频生成任务适配器。
// 提交：POST /v2/video_generation；查询：GET /v2/query/video_generation/{task_id}。
// 计费：ModelPrice 为 768P 每秒单价，2K 档按官方价比 ×(0.13/0.08)；
// 结算按 usage.total_seconds（参考视频按输入时长与输出同价计费，total_seconds 已包含）。

var ModelList = []string{
	"MiniMax-H3",
}

const ChannelName = "submodel"

// h3ResolutionRatios 分辨率档相对 768P 基准价的计费倍率（官方 768P $0.08/s、2K $0.13/s）
var h3ResolutionRatios = map[string]float64{
	"768P": 1,
	"2K":   0.13 / 0.08,
}

const (
	h3MinDurationSeconds     = 4
	h3MaxDurationSeconds     = 15
	h3DefaultDurationSeconds = 5
)

type h3CreateRequest struct {
	Model         string                        `json:"model"`
	Content       []relaycommon.TaskContentItem `json:"content"`
	Resolution    string                        `json:"resolution"`
	Duration      int                           `json:"duration"`
	Ratio         string                        `json:"ratio,omitempty"`
	AigcWatermark *bool                         `json:"aigc_watermark,omitempty"`
}

// h3Metadata 平铺 metadata 覆盖项（与统一视频请求的 metadata 写法一致）
type h3Metadata struct {
	Content       []relaycommon.TaskContentItem `json:"content,omitempty"`
	Resolution    *string                       `json:"resolution,omitempty"`
	Ratio         *string                       `json:"ratio,omitempty"`
	Duration      *int                          `json:"duration,omitempty"`
	AigcWatermark *bool                         `json:"aigc_watermark,omitempty"`
}

type h3Error struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

type h3SubmitResponse struct {
	TaskID    string   `json:"task_id"`
	Type      string   `json:"type,omitempty"`
	Error     *h3Error `json:"error,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

type h3TaskContent struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

type h3Usage struct {
	TotalSeconds    int `json:"total_seconds,omitempty"`
	InputSeconds    int `json:"input_seconds,omitempty"`
	OutputSeconds   int `json:"output_seconds,omitempty"`
	InputImageCount int `json:"input_image_count,omitempty"`
}

type h3Progress struct {
	Stage   string `json:"stage,omitempty"`
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message,omitempty"`
}

type h3Task struct {
	ID         string         `json:"id,omitempty"`
	Model      string         `json:"model,omitempty"`
	Status     string         `json:"status,omitempty"`
	Resolution string         `json:"resolution,omitempty"`
	Duration   int            `json:"duration,omitempty"`
	Ratio      string         `json:"ratio,omitempty"`
	Content    *h3TaskContent `json:"content,omitempty"`
	Usage      *h3Usage       `json:"usage,omitempty"`
	Error      *h3Error       `json:"error,omitempty"`
	Progress   *h3Progress    `json:"progress,omitempty"`
}

type h3QueryResponse struct {
	Task h3Task `json:"task"`
}

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
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	return relaycommon.ValidateMultipartDirect(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v2/video_generation", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	h3Req, err := convertToH3Request(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_h3_request_failed")
	}
	logger.LogJson(c, "submodel h3 request body", h3Req)

	bodyBytes, err := common.Marshal(h3Req)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_h3_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

func normalizeH3Resolution(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return s
}

func convertToH3Request(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*h3CreateRequest, error) {
	req.NormalizeForCompatibility()

	upstreamModel := req.Model
	if info != nil && info.IsModelMapped {
		upstreamModel = info.UpstreamModelName
	}

	var meta h3Metadata
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &meta); err != nil {
		return nil, err
	}

	// 分辨率：metadata.resolution > size 字段 > 默认 768P
	resolution := "768P"
	if req.Size != "" {
		resolution = normalizeH3Resolution(req.Size)
	}
	if meta.Resolution != nil {
		resolution = normalizeH3Resolution(*meta.Resolution)
	}
	if _, ok := h3ResolutionRatios[resolution]; !ok {
		return nil, fmt.Errorf("invalid resolution %q, supported: 768P, 2K", resolution)
	}

	// 时长：metadata.duration > duration > seconds > 默认 5，上游范围 4-15 秒
	duration := req.Duration
	if duration == 0 && req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		}
		duration = seconds
	}
	if meta.Duration != nil {
		duration = *meta.Duration
	}
	if duration == 0 {
		duration = h3DefaultDurationSeconds
	}
	if duration < h3MinDurationSeconds || duration > h3MaxDurationSeconds {
		return nil, fmt.Errorf("duration must be between %d and %d seconds", h3MinDurationSeconds, h3MaxDurationSeconds)
	}

	// 输入内容：metadata.content 优先（顶层 content[] 已由 NormalizeForCompatibility 注入），
	// 否则由 prompt + image/images 组装
	content := meta.Content
	if len(content) == 0 {
		if strings.TrimSpace(req.Prompt) != "" {
			content = append(content, relaycommon.TaskContentItem{Type: "text", Text: req.Prompt})
		}
		images := req.Images
		if len(images) == 0 && strings.TrimSpace(req.InputReference) != "" {
			images = []string{req.InputReference}
		}
		if len(images) == 1 {
			content = append(content, relaycommon.TaskContentItem{
				Type: "image_url", Role: "first_frame",
				ImageURL: &relaycommon.TaskMediaURL{URL: images[0]},
			})
		} else {
			for _, img := range images {
				content = append(content, relaycommon.TaskContentItem{
					Type: "image_url", Role: "reference_image",
					ImageURL: &relaycommon.TaskMediaURL{URL: img},
				})
			}
		}
	}
	hasText := false
	for _, item := range content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			hasText = true
			break
		}
	}
	if !hasText {
		return nil, errors.New("h3 video generation requires a non-empty text prompt")
	}

	// 宽高比：文生视频必填且不能 adaptive，默认 16:9；含媒体输入时由上游自适应
	ratio := ""
	if meta.Ratio != nil {
		ratio = strings.TrimSpace(*meta.Ratio)
	}
	if ratio == "" && len(content) > 0 {
		textOnly := true
		for _, item := range content {
			if item.Type != "text" {
				textOnly = false
				break
			}
		}
		if textOnly {
			ratio = "16:9"
		}
	}

	return &h3CreateRequest{
		Model:         upstreamModel,
		Content:       content,
		Resolution:    resolution,
		Duration:      duration,
		Ratio:         ratio,
		AigcWatermark: meta.AigcWatermark,
	}, nil
}

// EstimateBilling 预扣倍率：秒数 × 分辨率档
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	h3Req, err := convertToH3Request(info, taskReq)
	if err != nil {
		return nil
	}

	otherRatios := map[string]float64{
		"seconds": float64(min(h3Req.Duration, relaycommon.MaxTaskDurationSeconds)),
	}
	if ratio, ok := h3ResolutionRatios[h3Req.Resolution]; ok && ratio != 1.0 {
		otherRatios["resolution-"+h3Req.Resolution] = ratio
	}
	return otherRatios
}

// AdjustBillingOnComplete 按上游 usage.total_seconds 与实际分辨率重算额度。
// 参考视频输入按其时长与输出同价计费，total_seconds 已含输入+输出秒数。
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, _ *relaycommon.TaskInfo) int {
	bc := task.PrivateData.BillingContext
	if bc == nil || bc.ModelPrice <= 0 {
		return 0
	}
	var queryResp h3QueryResponse
	if err := common.Unmarshal(task.Data, &queryResp); err != nil {
		return 0
	}
	usage := queryResp.Task.Usage
	if usage == nil || usage.TotalSeconds <= 0 {
		return 0
	}
	resRatio := 1.0
	if r, ok := h3ResolutionRatios[normalizeH3Resolution(queryResp.Task.Resolution)]; ok {
		resRatio = r
	}
	quota, clamp := common.QuotaFromFloatChecked(bc.ModelPrice * common.QuotaPerUnit * bc.GroupRatio * resRatio * float64(usage.TotalSeconds))
	if clamp != nil {
		common.SysError(fmt.Sprintf("submodel h3 settle quota clamped: task=%s original=%f clamped=%d", task.TaskID, clamp.Original, clamp.Clamped))
	}
	return quota
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var submitResp h3SubmitResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if submitResp.Error != nil {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", submitResp.Error.Type, submitResp.Error.Message), "h3_api_error", resp.StatusCode)
		return
	}
	if submitResp.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty, body: %s", responseBody), "invalid_response", http.StatusInternalServerError)
		return
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = dto.VideoStatusQueued
	openAIResp.CreatedAt = common.GetTimestamp()
	c.JSON(http.StatusOK, openAIResp)

	return submitResp.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v2/query/video_generation/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var queryResp h3QueryResponse
	if err := common.Unmarshal(respBody, &queryResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	task := queryResp.Task
	taskResult := relaycommon.TaskInfo{
		Code:       0,
		TaskID:     task.ID,
		Resolution: task.Resolution,
	}
	if task.Progress != nil {
		taskResult.Progress = fmt.Sprintf("%d%%", task.Progress.Percent)
	}

	switch task.Status {
	case "queued":
		taskResult.Status = model.TaskStatusQueued
	case "running":
		taskResult.Status = model.TaskStatusInProgress
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		if task.Content != nil {
			taskResult.Url = task.Content.URL
		}
	case "failed", "cancelled":
		taskResult.Status = model.TaskStatusFailure
		if task.Error != nil && task.Error.Message != "" {
			taskResult.Reason = task.Error.Message
		} else {
			taskResult.Reason = "task " + task.Status
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var queryResp h3QueryResponse
	if err := common.Unmarshal(task.Data, &queryResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal h3 task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertH3Status(queryResp.Task.Status)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	if queryResp.Task.Content != nil && queryResp.Task.Content.URL != "" {
		openAIResp.SetMetadata("url", queryResp.Task.Content.URL)
	}
	if queryResp.Task.Error != nil {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    queryResp.Task.Error.Code,
			Message: queryResp.Task.Error.Message,
		}
	}

	return common.Marshal(openAIResp)
}

func convertH3Status(status string) string {
	switch status {
	case "queued":
		return dto.VideoStatusQueued
	case "running":
		return dto.VideoStatusInProgress
	case "succeeded":
		return dto.VideoStatusCompleted
	case "failed", "cancelled":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}
