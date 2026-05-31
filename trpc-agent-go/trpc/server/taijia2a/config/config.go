package config

import (
	"sync"

	a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
)

var (
	configer = NewLocalConfiger()
	mutex    sync.RWMutex
)

// Configer configer interface.
type Configer interface {
	GetProxyConfig(name string) (*ProxyConfig, bool)
}

// ProxyConfig proxy config.
type ProxyConfig struct {
	Name           string              `json:"name"`
	ProxyAgentCard a2aserver.AgentCard `json:"proxy_agent_card"`
	AgentID        string              `json:"agent_id"`
	RemoteTarget   string              `json:"remote_target"`
	Path           string              `json:"path"`
	Authorization  string              `json:"authorization"`
}

// RegisterConfiger register configer. default: localConfiger
func RegisterConfiger(c Configer) {
	mutex.Lock()
	defer mutex.Unlock()
	if c != nil {
		configer = c
	}
}

// GetConfiger get proxy config.
func GetConfiger() Configer {
	mutex.RLock()
	defer mutex.RUnlock()
	return configer
}
