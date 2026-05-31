// Package rainbow 包含七彩石配置
package rainbow

import (
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/business/onlinecfg"
)

// Init 初始化在线配置
func Init() error {
	if err := onlinecfg.LoadCfg("rainbow1", "config.toml", AppConfig{}); err != nil {
		log.Errorf("Load online config failed: %v", err)
		return err
	}

	log.Infof("load rainbow cfg: %+v", onlinecfg.GetCfg())
	return nil
}

// GetCfg 获取配置，如果获取失败，则取默认值
func GetCfg() AppConfig {
	if cfg, ok := onlinecfg.GetCfg().(*AppConfig); ok {
		return *cfg
	}
	return AppConfig{}
}
