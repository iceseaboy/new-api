package doubao

import "strings"

var ModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
}

var ChannelName = "doubao-video"

// videoPricing 视频生成模型的实际单价（元/百万 token，在线推理），
// 按"输出分辨率档 × 输入是否含视频"区分定价。
// base 为基准价（低分辨率档 480p/720p + 不含视频），管理员应将该模型的
// 输入价格(ModelRatio)配置为 base；系统按 实际单价/base 自动乘以折扣比率，
// 使最终单价精确等于上游价目表。
type videoPricing struct {
	base         float64 // 低分辨率(480p/720p) + 不含视频
	lowResVideo  float64 // 低分辨率 + 含视频
	supports1080 bool    // 是否支持 1080p 输出（fast 不支持）
	highResNoVid float64 // 1080p + 不含视频
	highResVideo float64 // 1080p + 含视频
	supports4k   bool    // 是否支持 4k 输出（fast 不支持）
	fourKNoVid   float64 // 4k + 不含视频
	fourKVideo   float64 // 4k + 含视频
}

// seedance2Pricing / seedance2FastPricing 为 Seedance 2.0 标准版/快速版的定价。
// 火山原生模型 id（doubao-seedance-2-0-260128）与 zlhub 对外模型名（doubao-seedance-2.0）
// 指向同一档价格，这里共用同一份定价值，避免两个别名漂移。
var (
	seedance2Pricing = videoPricing{
		base:         46,
		lowResVideo:  28,
		supports1080: true,
		highResNoVid: 51,
		highResVideo: 31,
		supports4k:   true,
		fourKNoVid:   26,
		fourKVideo:   16,
	}
	seedance2FastPricing = videoPricing{
		base:        37,
		lowResVideo: 22,
		// 不支持 1080p/4k 输出
	}
)

var videoPricingMap = map[string]videoPricing{
	"doubao-seedance-2-0-260128":      seedance2Pricing,
	"doubao-seedance-2.0":             seedance2Pricing,
	"doubao-seedance-2-0-fast-260128": seedance2FastPricing,
	"doubao-seedance-2.0-fast":        seedance2FastPricing,
}

// GetVideoBillingRatio 返回相对基准价(base)的计费比率，用于乘到基础额度上。
// resolution 为请求中的输出分辨率（"480p"/"720p"/"1080p"/"4k"，大小写不敏感，
// 空字符串按默认 720p 档处理）；hasVideo 表示输入是否包含视频。
// 无对应定价时返回 (0, false)。
func GetVideoBillingRatio(modelName, resolution string, hasVideo bool) (float64, bool) {
	p, ok := videoPricingMap[modelName]
	if !ok || p.base <= 0 {
		return 0, false
	}
	res := strings.ToLower(strings.TrimSpace(resolution))
	is1080 := p.supports1080 && res == "1080p"
	is4k := p.supports4k && res == "4k"
	var price float64
	switch {
	case is4k && hasVideo:
		price = p.fourKVideo
	case is4k:
		price = p.fourKNoVid
	case is1080 && hasVideo:
		price = p.highResVideo
	case is1080:
		price = p.highResNoVid
	case hasVideo:
		price = p.lowResVideo
	default:
		price = p.base
	}
	return price / p.base, true
}
