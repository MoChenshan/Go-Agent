package main

import (
	"encoding/json"
	"fmt"
	"os"

	taijiconf "git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/config"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

func loadProxyConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read config file: %v", err)
	}

	var cfgs []taijiconf.ProxyConfig
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return fmt.Errorf("parse json config: %v", err)
	}

	log.Debugf("config: %+v", cfgs)
	taijiconf.CoverLocalConfig(taijiconf.GetConfiger(), cfgs)
	return nil
}

// wujiConfigImpl wuji config
type wujiConfigImpl struct{}

// GetProxyConfig get proxy config
func (c wujiConfigImpl) GetProxyConfig(name string) (*taijiconf.ProxyConfig, bool) {
	// config from wuji
	return nil, false
}
