package config

import (
	"sync"

	"git.code.oa.com/trpc-go/trpc-go/log"
)

type localConfiger struct {
	proxyCfgs sync.Map // key: name, value: conf.ProxyConfig
}

// NewLocalConfiger create local configer.
func NewLocalConfiger() Configer {
	return &localConfiger{proxyCfgs: sync.Map{}}
}

// GetProxyConfig get proxy config.
func (c *localConfiger) GetProxyConfig(name string) (*ProxyConfig, bool) {
	if cfg, ok := c.proxyCfgs.Load(name); ok {
		if cfg, ok := cfg.(ProxyConfig); ok {
			return &cfg, true
		}
	}
	return nil, false
}

// CoverLocalConfig cover local config.
// Configer must be an instance created by the NewLocalConfiger() function.
func CoverLocalConfig(conf Configer, cfgs []ProxyConfig) {
	c, ok := conf.(*localConfiger)
	if !ok {
		log.Errorf("conf is not local configer")
		return
	}

	// delete old
	newNames := make(map[string]bool, len(cfgs))
	for i := 0; i < len(cfgs); i++ {
		newNames[cfgs[i].Name] = true
	}
	c.proxyCfgs.Range(func(key, value any) bool {
		if _, ok := newNames[key.(string)]; !ok {
			c.proxyCfgs.Delete(key.(string))
		}
		return true
	})

	// add new or update existed config
	for i := 0; i < len(cfgs); i++ {
		c.proxyCfgs.Store(cfgs[i].Name, cfgs[i])
	}
}

// UpdateLocalConfig update local config. Add new or update existed config.
// Configer must be an instance created by the NewLocalConfiger() function.
func UpdateLocalConfig(conf Configer, cfgs []ProxyConfig) {
	c, ok := conf.(*localConfiger)
	if !ok {
		log.Errorf("conf is not local configer")
		return
	}
	for i := 0; i < len(cfgs); i++ {
		c.proxyCfgs.Store(cfgs[i].Name, cfgs[i])
	}
}

// DeleteLocalConfig delete local config. Delete old config.
// Configer must be an instance created by the NewLocalConfiger() function.
func DeleteLocalConfig(conf Configer, cfgs []ProxyConfig) {
	c, ok := conf.(*localConfiger)
	if !ok {
		log.Errorf("conf is not local configer")
		return
	}
	for i := 0; i < len(cfgs); i++ {
		if _, ok := c.proxyCfgs.Load(cfgs[i].Name); ok {
			c.proxyCfgs.Delete(cfgs[i].Name)
		}
	}
}
