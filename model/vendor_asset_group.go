package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// VendorAssetGroup 记录上游素材组的归属（多租户隔离）。
// 只记录归属关系，不存素材组的名称/状态等业务数据，实时信息以上游为准。
type VendorAssetGroup struct {
	Id           int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	UserId       int    `json:"user_id" gorm:"index:idx_vag_user_ch,priority:1;index:idx_vag_user_ch_group,priority:1"`
	TokenId      int    `json:"token_id"`
	ChannelId    int    `json:"channel_id" gorm:"index:idx_vag_user_ch,priority:2;index:idx_vag_user_ch_group,priority:2;uniqueIndex:idx_vag_ch_group,priority:1"`
	AssetGroupId string `json:"asset_group_id" gorm:"type:varchar(191);uniqueIndex:idx_vag_ch_group,priority:2;index:idx_vag_user_ch_group,priority:3"`
	CreatedAt    int64  `json:"created_at"`
}

func (VendorAssetGroup) TableName() string {
	return "vendor_asset_groups"
}

// Insert 创建素材组归属记录（含幂等处理）。
// alreadyExists=true 表示记录已存在且 owner 一致（幂等成功）。
// 项目未开启 GORM TranslateError，唯一索引冲突的错误类型因数据库驱动而异，
// 因此创建失败后统一回查判断是否为重复记录，兼容 SQLite/MySQL/PostgreSQL。
func (g *VendorAssetGroup) Insert() (alreadyExists bool, err error) {
	createErr := DB.Create(g).Error
	if createErr == nil {
		return false, nil
	}
	var existing VendorAssetGroup
	if dbErr := DB.Where("channel_id = ? AND asset_group_id = ?",
		g.ChannelId, g.AssetGroupId).First(&existing).Error; dbErr != nil {
		if errors.Is(dbErr, gorm.ErrRecordNotFound) {
			// 不存在同键记录，说明不是唯一索引冲突，返回原始创建错误
			return false, createErr
		}
		return false, dbErr
	}
	if existing.UserId != g.UserId {
		return false, fmt.Errorf("asset group %s already owned by another user", g.AssetGroupId)
	}
	return true, nil
}

// CheckAssetGroupOwnership 校验素材组归属（多租户隔离：userId + channelId）。
// 返回 (owned, error)，error != nil 表示 DB 故障，调用方应按服务异常处理。
func CheckAssetGroupOwnership(userId, channelId int, assetGroupId string) (bool, error) {
	var count int64
	err := DB.Model(&VendorAssetGroup{}).
		Where("user_id = ? AND channel_id = ? AND asset_group_id = ?",
			userId, channelId, assetGroupId).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
