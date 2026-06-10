package seedance

var ChannelName = "seedance"

// Seedance 素材资产 API 的内部虚拟模型名。
// 素材接口免费且与具体生成模型无关，三个端点（CreateAssetGroup / CreateAsset / GetAsset）
// 共用同一个虚拟模型，仅用于令牌模型权限校验与渠道选路：
// 管理员在承载素材 API 的渠道上添加 seedance-asset 模型即可启用。
const ModelSeedanceAsset = "seedance-asset"

var AssetModelList = []string{
	ModelSeedanceAsset,
}
