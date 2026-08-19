package submodel

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/opclink/common"
	"github.com/QuantumNous/opclink/constant"
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
// 提交：POST /v2/video_generation；再生成：POST /v2/video_regeneration（走标准
// remix 路由 /v1/videos/{task_id}/remix）；Context-IR：POST /v2/h3_context_ir
// （模型名 MiniMax-H3-context-ir）；查询：GET /v2/query/video_generation/{task_id}。
//
// 计费（官方刊例价，人民币 ÷7.2 为美元系数，ModelPrice 为 768P 生成每秒基准价 ¥0.50/s）：
// - 生成：768P ¥0.50/s、2K ¥0.80/s（×1.6）；参考视频按输入时长与输出同价，
//   usage.total_seconds 已含；图片超 5 张每张 ¥0.20（折 0.4 个基准秒，结算期计）
// - 再生成（768P→2K）：¥0.30/s（×0.6），含原任务输入视频秒数；图片超 5 张每张
//   ¥0.15（折 0.3 个基准秒）
// - Context-IR：按 token 结算，输入 ¥5.8/百万、输出 ¥23/百万；ModelPrice 仅作按次预扣

var ModelList = []string{
	"MiniMax-H3",
	"MiniMax-H3-context-ir",
}

const ChannelName = "submodel"

// h3UpstreamModel 上游统一模型标识（Context-IR 网关侧用独立模型名区分计费与路由）
const h3UpstreamModel = "MiniMax-H3"

const h3ContextIRModel = "MiniMax-H3-context-ir"

// h3ResolutionRatios 分辨率档相对 768P 基准价的计费倍率（官方 768P ¥0.50/s、2K ¥0.80/s）
var h3ResolutionRatios = map[string]float64{
	"768P": 1,
	"2K":   0.8 / 0.5,
}

const (
	h3MinDurationSeconds     = 4
	h3MaxDurationSeconds     = 15
	h3DefaultDurationSeconds = 5

	// 再生成每秒价相对生成基准价的倍率（¥0.30 / ¥0.50）
	h3RegenRateRatio = 0.3 / 0.5
	// 免费输入图片张数（生成与再生成一致）
	h3FreeInputImageCount = 5
	// 超量图片折算成基准秒的当量：生成 ¥0.20/张、再生成 ¥0.15/张
	h3GenExtraImageSecondsEq   = 0.2 / 0.5
	h3RegenExtraImageSecondsEq = 0.15 / 0.5
	// Context-IR 每百万 token 美元价（官方 ¥5.8 / ¥23 ÷ 7.2）
	h3ContextIRPromptUSDPerM     = 5.8 / 7.2
	h3ContextIRCompletionUSDPerM = 23.0 / 7.2
)

type h3CreateRequest struct {
	Model         string                        `json:"model"`
	Content       []relaycommon.TaskContentItem `json:"content"`
	Resolution    string                        `json:"resolution"`
	Duration      int                           `json:"duration"`
	Ratio         string                        `json:"ratio,omitempty"`
	AigcWatermark *bool                         `json:"aigc_watermark,omitempty"`
}

type h3RegenRequest struct {
	Model        string `json:"model"`
	SourceTaskID string `json:"source_task_id"`
	Resolution   string `json:"resolution"`
}

type h3ContextIRRequest struct {
	Model    string                        `json:"model"`
	Content  []relaycommon.TaskContentItem `json:"content"`
	Duration int                           `json:"duration,omitempty"`
	Ratio    string                        `json:"ratio,omitempty"`
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
	// Context-IR 任务返回 token 用量
	TotalTokens      int `json:"total_tokens,omitempty"`
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
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
	// 再生成（/v1/videos/{task_id}/remix）：源任务由框架 ResolveOriginTask 解析，
	// 请求体无需 prompt，宽松解析后入栈供 Build/Estimate 复用
	if info.Action == constant.TaskActionRemix {
		var req relaycommon.TaskSubmitReq
		_ = common.UnmarshalBodyReusable(c, &req)
		if req.Model == "" {
			req.Model = info.OriginModelName
		}
		c.Set("task_request", req)
		return nil
	}
	return relaycommon.ValidateMultipartDirect(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.Action == constant.TaskActionRemix {
		return fmt.Sprintf("%s/v2/video_regeneration", a.baseURL), nil
	}
	if info.OriginModelName == h3ContextIRModel {
		return fmt.Sprintf("%s/v2/h3_context_ir", a.baseURL), nil
	}
	return fmt.Sprintf("%s/v2/video_generation", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	var payload any
	switch {
	case info.Action == constant.TaskActionRemix:
		regenReq, err := buildRegenRequest(info)
		if err != nil {
			return nil, err
		}
		payload = regenReq
	case info.OriginModelName == h3ContextIRModel:
		taskReq, err := relaycommon.GetTaskRequest(c)
		if err != nil {
			return nil, errors.Wrap(err, "get_task_request_failed")
		}
		ctxReq, err := convertToContextIRRequest(taskReq)
		if err != nil {
			return nil, errors.Wrap(err, "convert_to_context_ir_request_failed")
		}
		payload = ctxReq
	default:
		taskReq, err := relaycommon.GetTaskRequest(c)
		if err != nil {
			return nil, errors.Wrap(err, "get_task_request_failed")
		}
		h3Req, err := convertToH3Request(info, taskReq)
		if err != nil {
			return nil, errors.Wrap(err, "convert_to_h3_request_failed")
		}
		payload = h3Req
	}
	logger.LogJson(c, "submodel h3 request body", payload)

	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_h3_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

// buildRegenRequest 由框架解析出的源任务（info.OriginTaskID 为本站任务 ID）
// 构造再生成请求；上游要求其自身的任务 ID。
func buildRegenRequest(info *relaycommon.RelayInfo) (*h3RegenRequest, error) {
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return nil, errors.Wrap(err, "get_origin_task_failed")
	}
	if !exist || originTask == nil {
		return nil, errors.New("origin task not found")
	}
	upstreamID := originTask.GetUpstreamTaskID()
	if upstreamID == "" {
		return nil, errors.New("origin task has no upstream task id")
	}
	return &h3RegenRequest{
		Model:        h3UpstreamModel,
		SourceTaskID: upstreamID,
		Resolution:   "2K",
	}, nil
}

// convertToContextIRRequest 构造上下文理解任务请求（输出为增强后的提示词文本）
func convertToContextIRRequest(req relaycommon.TaskSubmitReq) (*h3ContextIRRequest, error) {
	req.NormalizeForCompatibility()

	var meta h3Metadata
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &meta); err != nil {
		return nil, err
	}

	content := meta.Content
	if len(content) == 0 {
		if strings.TrimSpace(req.Prompt) != "" {
			content = append(content, relaycommon.TaskContentItem{Type: "text", Text: req.Prompt})
		}
		for _, img := range req.Images {
			content = append(content, relaycommon.TaskContentItem{
				Type: "image_url", Role: "reference_image",
				ImageURL: &relaycommon.TaskMediaURL{URL: img},
			})
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
		return nil, errors.New("h3 context ir requires a non-empty text prompt")
	}

	duration := req.Duration
	if meta.Duration != nil {
		duration = *meta.Duration
	}
	if duration != 0 && (duration < h3MinDurationSeconds || duration > h3MaxDurationSeconds) {
		return nil, fmt.Errorf("duration must be between %d and %d seconds", h3MinDurationSeconds, h3MaxDurationSeconds)
	}
	ratio := ""
	if meta.Ratio != nil {
		ratio = strings.TrimSpace(*meta.Ratio)
	}

	return &h3ContextIRRequest{
		Model:    h3UpstreamModel,
		Content:  content,
		Duration: duration,
		Ratio:    ratio,
	}, nil
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

// EstimateBilling 预扣倍率。生成：秒数 × 分辨率档；再生成：源任务时长 × 0.6；
// Context-IR：按 ModelPrice 按次预扣（结算期按 token 精确重算）。
// 超量图片加价在提交时不可靠预知，统一由结算差额补收。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if info.OriginModelName == h3ContextIRModel {
		return nil
	}
	if info.Action == constant.TaskActionRemix {
		seconds := h3DefaultDurationSeconds
		if originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID); err == nil && exist && originTask != nil {
			var originResp h3QueryResponse
			if err := common.Unmarshal(originTask.Data, &originResp); err == nil && originResp.Task.Duration > 0 {
				seconds = originResp.Task.Duration
			}
		}
		return map[string]float64{
			"seconds":    float64(min(seconds, relaycommon.MaxTaskDurationSeconds)),
			"regen_rate": h3RegenRateRatio,
		}
	}

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

// AdjustBillingOnComplete 结算期按上游 usage 精确重算额度。
// 生成/再生成按 total_seconds（含参考视频输入秒数）+ 超量图片秒当量；
// Context-IR 按 prompt/completion token 与官方单价重算。
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, _ *relaycommon.TaskInfo) int {
	bc := task.PrivateData.BillingContext
	if bc == nil {
		return 0
	}
	var queryResp h3QueryResponse
	if err := common.Unmarshal(task.Data, &queryResp); err != nil {
		return 0
	}
	usage := queryResp.Task.Usage
	if usage == nil {
		return 0
	}

	modelName := task.Properties.OriginModelName
	if modelName == "" {
		modelName = task.Properties.UpstreamModelName
	}

	var rawCost float64
	switch {
	case modelName == h3ContextIRModel:
		if usage.PromptTokens <= 0 && usage.CompletionTokens <= 0 {
			return 0
		}
		rawCost = (float64(usage.PromptTokens)*h3ContextIRPromptUSDPerM +
			float64(usage.CompletionTokens)*h3ContextIRCompletionUSDPerM) / 1_000_000 *
			common.QuotaPerUnit * bc.GroupRatio
	case task.Action == constant.TaskActionRemix:
		if bc.ModelPrice <= 0 || usage.TotalSeconds <= 0 {
			return 0
		}
		extraImages := max(0, usage.InputImageCount-h3FreeInputImageCount)
		secondsEq := h3RegenRateRatio*float64(usage.TotalSeconds) +
			h3RegenExtraImageSecondsEq*float64(extraImages)
		rawCost = bc.ModelPrice * common.QuotaPerUnit * bc.GroupRatio * secondsEq
	default:
		if bc.ModelPrice <= 0 || usage.TotalSeconds <= 0 {
			return 0
		}
		resRatio := 1.0
		if r, ok := h3ResolutionRatios[normalizeH3Resolution(queryResp.Task.Resolution)]; ok {
			resRatio = r
		}
		extraImages := max(0, usage.InputImageCount-h3FreeInputImageCount)
		secondsEq := resRatio*float64(usage.TotalSeconds) +
			h3GenExtraImageSecondsEq*float64(extraImages)
		rawCost = bc.ModelPrice * common.QuotaPerUnit * bc.GroupRatio * secondsEq
	}

	quota, clamp := common.QuotaFromFloatChecked(rawCost)
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
		if task.Usage != nil {
			taskResult.CompletionTokens = task.Usage.CompletionTokens
			taskResult.TotalTokens = task.Usage.TotalTokens
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
	if queryResp.Task.Content != nil && queryResp.Task.Content.Prompt != "" {
		// Context-IR 任务的结果是增强后的提示词文本
		openAIResp.SetMetadata("prompt", queryResp.Task.Content.Prompt)
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
