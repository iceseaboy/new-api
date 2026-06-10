package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// VendorAsset 记录上游素材的归属（多租户隔离）。
// 只记录归属关系，不存素材的名称/状态/URL 等业务数据，实时信息以上游为准。
type VendorAsset struct {
	Id           int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	UserId       int    `json:"user_id" gorm:"index:idx_va_user_ch,priority:1;index:idx_va_user_ch_asset,priority:1"`
	TokenId      int    `json:"token_id"`
	ChannelId    int    `json:"channel_id" gorm:"index:idx_va_user_ch,priority:2;index:idx_va_user_ch_asset,priority:2;uniqueIndex:idx_va_ch_asset,priority:1;uniqueIndex:idx_va_ch_group_asset,priority:1"`
	AssetId      string `json:"asset_id" gorm:"type:varchar(191);uniqueIndex:idx_va_ch_asset,priority:2;uniqueIndex:idx_va_ch_group_asset,priority:3;index:idx_va_user_ch_asset,priority:3"`
	AssetGroupId string `json:"asset_group_id" gorm:"type:varchar(191);uniqueIndex:idx_va_ch_group_asset,priority:2"`
	AssetType    string `json:"asset_type" gorm:"type:varchar(30)"`
	CreatedAt    int64  `json:"created_at"`
}

func (VendorAsset) TableName() string {
	return "vendor_assets"
}

// Insert 创建素材归属记录（含幂等处理）。
// alreadyExists=true 表示记录已存在且 owner 一致（幂等成功）。
// 与 VendorAssetGroup.Insert 相同：创建失败后回查判断是否为重复记录。
func (a *VendorAsset) Insert() (alreadyExists bool, err error) {
	createErr := DB.Create(a).Error
	if createErr == nil {
		return false, nil
	}
	var existing VendorAsset
	if dbErr := DB.Where("channel_id = ? AND asset_id = ?",
		a.ChannelId, a.AssetId).First(&existing).Error; dbErr != nil {
		if errors.Is(dbErr, gorm.ErrRecordNotFound) {
			return false, createErr
		}
		return false, dbErr
	}
	if existing.UserId != a.UserId {
		return false, fmt.Errorf("asset %s already owned by another user", a.AssetId)
	}
	return true, nil
}

// CheckAssetOwnership 校验素材归属（多租户隔离：userId + channelId）。
// 返回 (*VendorAsset, error)：
// asset=nil && error=nil 表示不属于当前用户；error != nil 表示 DB 故障。
func CheckAssetOwnership(userId, channelId int, assetId string) (*VendorAsset, error) {
	var asset VendorAsset
	err := DB.Where("user_id = ? AND channel_id = ? AND asset_id = ?",
		userId, channelId, assetId).First(&asset).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &asset, nil
}

// GetVendorAssets 本地素材列表（按 userId 隔离；channelId>0 时额外过滤 channel）
func GetVendorAssets(userId, channelId int, page, pageSize int) ([]*VendorAsset, int64, error) {
	var assets []*VendorAsset
	var total int64
	tx := DB.Where("user_id = ?", userId)
	if channelId > 0 {
		tx = tx.Where("channel_id = ?", channelId)
	}
	if err := tx.Model(&VendorAsset{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&assets).Error
	return assets, total, err
}
