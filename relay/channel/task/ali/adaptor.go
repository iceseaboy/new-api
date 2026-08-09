package ali

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
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// AliVideoRequest 阿里通义万相视频生成请求
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

// AliVideoMedia 媒体素材（Wan2.7 i2v 与 happyhorse 系列：first_frame / reference_image / video）
type AliVideoMedia struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// AliVideoInput 视频输入参数
type AliVideoInput struct {
	Prompt         string          `json:"prompt,omitempty"`          // 文本提示词
	ImgURL         string          `json:"img_url,omitempty"`         // 首帧图像URL或Base64（图生视频）
	FirstFrameURL  string          `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string          `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	AudioURL       string          `json:"audio_url,omitempty"`       // 音频URL（wan2.5支持）
	Media          []AliVideoMedia `json:"media,omitempty"`           // 媒体列表（wan2.7-i2v 新协议 / happyhorse i2v/r2v/video-edit）
	NegativePrompt string          `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string          `json:"template,omitempty"`        // 视频特效模板
}

// AliVideoParameters 视频参数
type AliVideoParameters struct {
	Resolution   string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P（图生视频、首尾帧生视频）
	Size         string `json:"size,omitempty"`          // 尺寸: 如 "832*480"（文生视频）
	Ratio        string `json:"ratio,omitempty"`         // 宽高比: 如 "16:9"（happyhorse t2v/r2v）
	Mode         string `json:"mode,omitempty"`          // 生成模式: std/pro（kling 系列）
	AspectRatio  string `json:"aspect_ratio,omitempty"`  // 宽高比: 如 "16:9"（kling 系列）
	Duration     int    `json:"duration,omitempty"`      // 时长: 3-15秒
	PromptExtend bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool  `json:"watermark,omitempty"`     // 是否添加水印（happyhorse 上游默认 true，需显式传 false 去水印）
	Audio        *bool  `json:"audio,omitempty"`         // 是否添加音频（wan2.5）
	Seed         int    `json:"seed,omitempty"`          // 随机数种子
}

// AliVideoResponse 阿里通义万相响应
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

// AliVideoOutput 输出信息
type AliVideoOutput struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	SubmitTime    string `json:"submit_time,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	OrigPrompt    string `json:"orig_prompt,omitempty"`
	ActualPrompt  string `json:"actual_prompt,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

// AliUsage 使用统计
type AliUsage struct {
	// duration 可能为小数（video-edit 计费时长 = 输入+输出，如 13.24），不能用 IntValue
	Duration   float64      `json:"duration,omitempty"`
	VideoCount dto.IntValue `json:"video_count,omitempty"`
	SR         dto.IntValue `json:"SR,omitempty"`
}

type AliMetadata struct {
	// Input 相关
	AudioURL       string          `json:"audio_url,omitempty"`       // 音频URL
	ImgURL         string          `json:"img_url,omitempty"`         // 图片URL（图生视频）
	FirstFrameURL  string          `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string          `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	Media          []AliVideoMedia `json:"media,omitempty"`           // 媒体列表（wan2.7-i2v新协议）
	NegativePrompt string          `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string          `json:"template,omitempty"`        // 视频特效模板

	// Parameters 相关
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Ratio        *string `json:"ratio,omitempty"`         // 宽高比: 如 "16:9"
	Mode         *string `json:"mode,omitempty"`          // 生成模式: std/pro（kling 系列）
	AspectRatio  *string `json:"aspect_ratio,omitempty"`  // 宽高比: 如 "16:9"（kling 系列）
	Duration     *int    `json:"duration,omitempty"`      // 时长
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
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
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	// ValidateMultipartDirect 负责解析并将原始 TaskSubmitReq 存入 context
	return relaycommon.ValidateMultipartDirect(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

// BuildRequestHeader sets required headers for Ali API
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_ali_request_failed")
	}
	logger.LogJson(c, "ali video request body", aliReq)

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
)

func sizeToResolution(size string) (string, error) {
	if lo.Contains(size480p, size) {
		return "480P", nil
	} else if lo.Contains(size720p, size) {
		return "720P", nil
	} else if lo.Contains(size1080p, size) {
		return "1080P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

// aliRatios 各模型分辨率档位相对基准档（模型基准价 ModelPrice 对应比值 1 的档）的计费倍率
var aliRatios = map[string]map[string]float64{
	"wan2.6-i2v": {
		"720P":  1,
		"1080P": 1 / 0.6,
	},
	"wan2.5-t2v-preview": {
		"480P":  1,
		"720P":  2,
		"1080P": 1 / 0.3,
	},
	"wan2.2-t2v-plus": {
		"480P":  1,
		"1080P": 0.7 / 0.14,
	},
	"wan2.5-i2v-preview": {
		"480P":  1,
		"720P":  2,
		"1080P": 1 / 0.3,
	},
	"wan2.2-i2v-plus": {
		"480P":  1,
		"1080P": 0.7 / 0.14,
	},
	"wan2.2-kf2v-flash": {
		"480P":  1,
		"720P":  2,
		"1080P": 4.8,
	},
	"wan2.2-i2v-flash": {
		"480P": 1,
		"720P": 2,
	},
	"wan2.2-s2v": {
		"480P": 1,
		"720P": 0.9 / 0.5,
	},
	// happyhorse t2v/i2v/r2v：基准 480P ¥0.27/s，720P ¥0.54/s，1080P ¥0.72/s
	"happyhorse-1.1-t2v": {
		"480P":  1,
		"720P":  2,
		"1080P": 0.72 / 0.27,
	},
	"happyhorse-1.1-i2v": {
		"480P":  1,
		"720P":  2,
		"1080P": 0.72 / 0.27,
	},
	"happyhorse-1.1-r2v": {
		"480P":  1,
		"720P":  2,
		"1080P": 0.72 / 0.27,
	},
	// happyhorse video-edit：基准 720P ¥0.72/s，1080P ¥1.28/s（无 480P）
	"happyhorse-1.0-video-edit": {
		"720P":  1,
		"1080P": 1.28 / 0.72,
	},
	// kling v3（百炼）：基准 720P 无声 ¥0.6/s，1080P 无声 ¥0.8/s；有声统一 ×1.5（见 aliAudioRatios）
	"kling/kling-v3-video-generation": {
		"720P":  1,
		"1080P": 0.8 / 0.6,
	},
}

// aliAudioRatios 音频维度倍率：parameters.audio=true 时在分辨率档之上叠乘。
// kling v3：720P 有声 ¥0.9=0.6×1.5，1080P 有声 ¥1.2=0.8×1.5，两档一致。
var aliAudioRatios = map[string]float64{
	"kling/kling-v3-video-generation": 1.5,
}

func ProcessAliOtherRatios(aliReq *AliVideoRequest) (map[string]float64, error) {
	otherRatios := make(map[string]float64)
	var resolution string

	// size match
	if aliReq.Parameters.Size != "" {
		toResolution, err := sizeToResolution(aliReq.Parameters.Size)
		if err != nil {
			return nil, err
		}
		resolution = toResolution
	} else {
		resolution = strings.ToUpper(aliReq.Parameters.Resolution)
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
	}
	if otherRatio, ok := aliRatios[aliReq.Model]; ok {
		if ratio, ok := otherRatio[resolution]; ok {
			otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
		}
	}
	if audioRatio, ok := aliAudioRatios[aliReq.Model]; ok {
		if aliReq.Parameters.Audio != nil && *aliReq.Parameters.Audio {
			otherRatios["audio"] = audioRatio
		}
	}
	return otherRatios, nil
}

func isWan27I2VModel(model string) bool {
	return strings.HasPrefix(model, "wan2.7-i2v")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstTaskImage(req relaycommon.TaskSubmitReq) string {
	if image := strings.TrimSpace(req.Image); image != "" {
		return image
	}
	for _, image := range req.Images {
		if trimmed := strings.TrimSpace(image); trimmed != "" {
			return trimmed
		}
	}
	if inputReference := strings.TrimSpace(req.InputReference); inputReference != "" {
		return inputReference
	}
	return ""
}

func secondTaskImage(req relaycommon.TaskSubmitReq) string {
	nonEmptyImages := 0
	for _, image := range req.Images {
		trimmed := strings.TrimSpace(image)
		if trimmed == "" {
			continue
		}
		nonEmptyImages++
		if nonEmptyImages == 2 {
			return trimmed
		}
	}
	return ""
}

func normalizeWan27I2VInput(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if !isWan27I2VModel(aliReq.Model) {
		return nil
	}

	if len(aliReq.Input.Media) == 0 {
		firstFrameURL := firstNonEmpty(aliReq.Input.FirstFrameURL, aliReq.Input.ImgURL, firstTaskImage(req))
		lastFrameURL := firstNonEmpty(aliReq.Input.LastFrameURL, secondTaskImage(req))
		audioURL := aliReq.Input.AudioURL

		if firstFrameURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "first_frame",
				URL:  firstFrameURL,
			})
		}
		if lastFrameURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "last_frame",
				URL:  lastFrameURL,
			})
		}
		if audioURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "driving_audio",
				URL:  audioURL,
			})
		}
	}

	if len(aliReq.Input.Media) == 0 {
		return fmt.Errorf("wan2.7-i2v requires image, images, input_reference, or input.media")
	}

	// Wan2.7 image-to-video uses the new input.media protocol. Avoid sending
	// legacy fields that belong to wan2.6 and earlier image-to-video APIs.
	aliReq.Input.ImgURL = ""
	aliReq.Input.FirstFrameURL = ""
	aliReq.Input.LastFrameURL = ""
	aliReq.Input.AudioURL = ""
	return nil
}

// normalizeResolution 归一化分辨率写法：480p/720p/1080p → 480P/720P/1080P
func normalizeResolution(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s != "" && !strings.HasSuffix(s, "P") {
		s += "P"
	}
	return s
}

func isHappyHorseModel(model string) bool {
	return strings.HasPrefix(model, "happyhorse")
}

// isKlingDashScopeModel 判断是否为百炼平台上的 kling 系列模型（如 kling/kling-v3-video-generation）
func isKlingDashScopeModel(model string) bool {
	return strings.HasPrefix(model, "kling/")
}

func (a *TaskAdaptor) convertToAliRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	upstreamModel := req.Model
	if info.IsModelMapped {
		upstreamModel = info.UpstreamModelName
	}
	isHappyHorse := isHappyHorseModel(req.Model)
	isKling := isKlingDashScopeModel(req.Model)
	aliReq := &AliVideoRequest{
		Model: upstreamModel,
		Input: AliVideoInput{
			Prompt: req.Prompt,
			ImgURL: firstTaskImage(req),
		},
		Parameters: &AliVideoParameters{
			PromptExtend: !isHappyHorse && !isKling, // wan 默认开启智能改写；happyhorse/kling 无此参数
		},
	}
	if isHappyHorse || isKling {
		// happyhorse/kling 上游默认加水印，转售场景默认显式关闭（metadata.watermark 可覆盖）
		watermarkOff := false
		aliReq.Parameters.Watermark = &watermarkOff
	}
	if isHappyHorse {
		aliReq.Input.ImgURL = "" // happyhorse 用 media 数组，不用 img_url
	}

	// 处理分辨率映射
	if req.Size != "" {
		// text to video size must be contained *（happyhorse 全系用 resolution 档位，不用 size）
		if !isHappyHorse && strings.Contains(req.Model, "t2v") && !strings.Contains(req.Size, "*") {
			return nil, fmt.Errorf("invalid size: %s, example: %s", req.Size, "1920*1080")
		}
		if !isHappyHorse && strings.Contains(req.Size, "*") {
			aliReq.Parameters.Size = req.Size
		} else {
			aliReq.Parameters.Resolution = normalizeResolution(req.Size)
		}
	} else {
		// 根据模型设置默认分辨率
		if isKling {
			// kling 系列不强制下发分辨率：上游默认 720P，计费亦按 720P 基准档
		} else if isHappyHorse {
			aliReq.Parameters.Resolution = "1080P" // happyhorse 各模式上游默认均为 1080P
		} else if strings.Contains(req.Model, "t2v") { // text to video
			if strings.HasPrefix(req.Model, "wan2.5") {
				aliReq.Parameters.Size = "1920*1080"
			} else if strings.HasPrefix(req.Model, "wan2.2") {
				aliReq.Parameters.Size = "1920*1080"
			} else {
				aliReq.Parameters.Size = "1280*720"
			}
		} else {
			if strings.HasPrefix(req.Model, "wan2.6") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(req.Model, "wan2.5") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(req.Model, "wan2.2-i2v-flash") {
				aliReq.Parameters.Resolution = "720P"
			} else if strings.HasPrefix(req.Model, "wan2.2-i2v-plus") {
				aliReq.Parameters.Resolution = "1080P"
			} else {
				aliReq.Parameters.Resolution = "720P"
			}
		}
	}

	// 处理时长（video-edit 无 duration 参数：输出时长跟随输入视频，不下发）
	isVideoEdit := strings.Contains(req.Model, "video-edit")
	if !isVideoEdit {
		if req.Duration > 0 {
			aliReq.Parameters.Duration = req.Duration
		} else if req.Seconds != "" {
			seconds, err := strconv.Atoi(req.Seconds)
			if err != nil {
				return nil, errors.Wrap(err, "convert seconds to int failed")
			} else {
				aliReq.Parameters.Duration = seconds
			}
		} else {
			aliReq.Parameters.Duration = 5 // 默认5秒
		}
	}

	// 从 metadata 中提取额外参数：
	// 1) 对象风格 {"input":{...},"parameters":{...}} 直接覆盖
	// 2) 平铺风格 {"resolution":..,"ratio":..,"media":[..]} 与 seedance/kling 文档写法一致
	if req.Metadata != nil {
		metadataBytes, err := common.Marshal(req.Metadata)
		if err != nil {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
		if err = common.Unmarshal(metadataBytes, aliReq); err != nil {
			return nil, errors.Wrap(err, "unmarshal metadata failed")
		}
		var meta AliMetadata
		if err = common.Unmarshal(metadataBytes, &meta); err == nil {
			applyFlatMetadata(aliReq, &meta)
		}
	}

	// happyhorse media 组装：metadata 未直接给 media 时，从统一请求字段推导
	if isHappyHorse && len(aliReq.Input.Media) == 0 {
		switch {
		case strings.Contains(req.Model, "-i2v"):
			img := req.Image
			if img == "" && len(req.Images) > 0 {
				img = req.Images[0]
			}
			if img == "" {
				img = req.InputReference
			}
			if img == "" {
				return nil, errors.New("happyhorse 图生视频需要提供首帧图（image 字段或 metadata.media type=first_frame）")
			}
			aliReq.Input.Media = []AliVideoMedia{{Type: "first_frame", URL: img}}
		case strings.Contains(req.Model, "-r2v"):
			images := req.Images
			if len(images) == 0 && req.Image != "" {
				images = []string{req.Image}
			}
			if len(images) == 0 {
				return nil, errors.New("happyhorse 参考生视频需要提供 1~9 张参考图（images 字段或 metadata.media type=reference_image）")
			}
			for _, u := range images {
				aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{Type: "reference_image", URL: u})
			}
		}
	}
	if isVideoEdit {
		hasVideo := false
		for _, m := range aliReq.Input.Media {
			if m.Type == "video" {
				hasVideo = true
				break
			}
		}
		if !hasVideo {
			return nil, errors.New("happyhorse 视频编辑需要 metadata.media 中包含 1 个 type=video 的待编辑视频")
		}
	}

	if aliReq.Model != upstreamModel {
		return nil, errors.New("can't change model with metadata")
	}

	if err := normalizeWan27I2VInput(aliReq, req); err != nil {
		return nil, err
	}

	return aliReq, nil
}

// applyFlatMetadata 将平铺风格的 metadata 键应用到请求（与 seedance/kling 文档的 metadata 写法保持一致）
func applyFlatMetadata(aliReq *AliVideoRequest, meta *AliMetadata) {
	if meta.ImgURL != "" {
		aliReq.Input.ImgURL = meta.ImgURL
	}
	if meta.FirstFrameURL != "" {
		aliReq.Input.FirstFrameURL = meta.FirstFrameURL
	}
	if meta.LastFrameURL != "" {
		aliReq.Input.LastFrameURL = meta.LastFrameURL
	}
	if meta.AudioURL != "" {
		aliReq.Input.AudioURL = meta.AudioURL
	}
	if meta.NegativePrompt != "" {
		aliReq.Input.NegativePrompt = meta.NegativePrompt
	}
	if meta.Template != "" {
		aliReq.Input.Template = meta.Template
	}
	if len(meta.Media) > 0 {
		aliReq.Input.Media = meta.Media
	}
	if meta.Resolution != nil {
		aliReq.Parameters.Resolution = normalizeResolution(*meta.Resolution)
	}
	if meta.Size != nil {
		aliReq.Parameters.Size = *meta.Size
	}
	if meta.Ratio != nil {
		aliReq.Parameters.Ratio = *meta.Ratio
	}
	if meta.Mode != nil {
		aliReq.Parameters.Mode = *meta.Mode
	}
	if meta.AspectRatio != nil {
		aliReq.Parameters.AspectRatio = *meta.AspectRatio
	}
	if meta.Duration != nil {
		aliReq.Parameters.Duration = *meta.Duration
	}
	if meta.PromptExtend != nil {
		aliReq.Parameters.PromptExtend = *meta.PromptExtend
	}
	if meta.Watermark != nil {
		aliReq.Parameters.Watermark = meta.Watermark
	}
	if meta.Audio != nil {
		aliReq.Parameters.Audio = meta.Audio
	}
	if meta.Seed != nil {
		aliReq.Parameters.Seed = *meta.Seed
	}
}

// EstimateBilling 根据用户请求参数计算 OtherRatios（时长、分辨率等）。
// 在 ValidateRequestAndSetAction 之后、价格计算之前调用。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil
	}

	seconds := aliReq.Parameters.Duration
	if seconds <= 0 {
		// video-edit 请求不带时长（输出跟随输入视频），先按 5 秒预估预扣，
		// 完成后由 AdjustBillingOnComplete 按上游 usage 实际时长差额结算
		seconds = 5
	}
	// metadata can override Duration past standard request validation;
	// cap it because it is used as a billing multiplier.
	otherRatios := map[string]float64{
		"seconds": float64(min(seconds, relaycommon.MaxTaskDurationSeconds)),
	}
	ratios, err := ProcessAliOtherRatios(aliReq)
	if err != nil {
		return otherRatios
	}
	for k, v := range ratios {
		otherRatios[k] = v
	}
	return otherRatios
}

// AdjustBillingOnComplete 按上游 usage 的实际时长与分辨率重算 happyhorse 任务额度。
// video-edit 的输出时长在提交时未知（跟随输入视频），必须结算期修正；
// 其余 happyhorse 模式同样以实际值为准。wan 系列维持原有按次逻辑（返回 0）。
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, _ *relaycommon.TaskInfo) int {
	modelName := task.Properties.OriginModelName
	if modelName == "" {
		modelName = task.Properties.UpstreamModelName
	}
	if !isHappyHorseModel(modelName) {
		return 0
	}
	bc := task.PrivateData.BillingContext
	if bc == nil || bc.ModelPrice <= 0 {
		return 0
	}
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil || aliResp.Usage == nil {
		return 0
	}
	duration := aliResp.Usage.Duration
	if duration <= 0 {
		return 0
	}
	resRatio := 1.0
	if m, ok := aliRatios[modelName]; ok {
		if r, ok2 := m[fmt.Sprintf("%dP", int(aliResp.Usage.SR))]; ok2 {
			resRatio = r
		}
	}
	return int(bc.ModelPrice * common.QuotaPerUnit * bc.GroupRatio * resRatio * duration)
}

// DoRequest delegates to common helper
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// 解析阿里响应
	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 检查错误
	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}

	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 转换为 OpenAI 格式响应
	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	// 返回 OpenAI 格式
	c.JSON(http.StatusOK, openAIResp)

	return aliResp.Output.TaskID, responseBody, nil
}

// FetchTask 查询任务状态
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)

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

// ParseTaskResult 解析任务结果
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 状态映射
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		// 阿里直接返回视频URL，不需要额外的代理端点
		taskResult.Url = aliResp.Output.VideoURL
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal ali response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	// 设置视频URL（核心字段）
	openAIResp.SetMetadata("url", aliResp.Output.VideoURL)

	// 错误处理
	if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Code,
			Message: aliResp.Message,
		}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Output.Code,
			Message: aliResp.Output.Message,
		}
	}

	return common.Marshal(openAIResp)
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}
