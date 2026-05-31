package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
)

// TestCoverLocalConfig_NormalCase 测试CoverLocalConfig正常情况
func TestCoverLocalConfig_NormalCase(t *testing.T) {
	configer := NewLocalConfiger()

	// 初始配置
	initialCfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1",
			RemoteTarget:   "target1",
			Path:           "/path1",
			Authorization:  "auth1",
		},
		{
			Name:           "config2",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent2",
			RemoteTarget:   "target2",
			Path:           "/path2",
			Authorization:  "auth2",
		},
	}

	// 先添加初始配置
	UpdateLocalConfig(configer, initialCfgs)

	// 验证初始配置存在
	cfg1, ok := configer.GetProxyConfig("config1")
	assert.True(t, ok)
	assert.Equal(t, "agent1", cfg1.AgentID)

	cfg2, ok := configer.GetProxyConfig("config2")
	assert.True(t, ok)
	assert.Equal(t, "agent2", cfg2.AgentID)

	// 新配置：更新config1，删除config2，添加config3
	newCfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1_updated",
			RemoteTarget:   "target1_updated",
			Path:           "/path1_updated",
			Authorization:  "auth1_updated",
		},
		{
			Name:           "config3",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent3",
			RemoteTarget:   "target3",
			Path:           "/path3",
			Authorization:  "auth3",
		},
	}

	// 执行覆盖操作
	CoverLocalConfig(configer, newCfgs)

	// 验证config1被更新
	cfg1Updated, ok := configer.GetProxyConfig("config1")
	assert.True(t, ok)
	assert.Equal(t, "agent1_updated", cfg1Updated.AgentID)
	assert.Equal(t, "target1_updated", cfg1Updated.RemoteTarget)

	// 验证config2被删除
	cfg2Deleted, ok := configer.GetProxyConfig("config2")
	assert.False(t, ok)
	assert.Nil(t, cfg2Deleted)

	// 验证config3被添加
	cfg3, ok := configer.GetProxyConfig("config3")
	assert.True(t, ok)
	assert.Equal(t, "agent3", cfg3.AgentID)
}

// TestCoverLocalConfig_EmptyConfig 测试CoverLocalConfig空配置情况
func TestCoverLocalConfig_EmptyConfig(t *testing.T) {
	configer := NewLocalConfiger()

	// 先添加一些配置
	initialCfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1",
			RemoteTarget:   "target1",
			Path:           "/path1",
			Authorization:  "auth1",
		},
	}
	UpdateLocalConfig(configer, initialCfgs)

	// 验证配置存在
	cfg, ok := configer.GetProxyConfig("config1")
	assert.True(t, ok)
	assert.NotNil(t, cfg)

	// 使用空配置覆盖
	CoverLocalConfig(configer, []ProxyConfig{})

	// 验证所有配置被删除
	cfgDeleted, ok := configer.GetProxyConfig("config1")
	assert.False(t, ok)
	assert.Nil(t, cfgDeleted)
}

// 创建一个非localConfiger的配置器
type fakeConfiger struct{}

func (f *fakeConfiger) GetProxyConfig(name string) (*ProxyConfig, bool) {
	return nil, false
}

// TestCoverLocalConfig_InvalidConfiger 测试CoverLocalConfig无效配置器
func TestCoverLocalConfig_InvalidConfiger(t *testing.T) {

	fakeConf := &fakeConfiger{}
	cfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1",
			RemoteTarget:   "target1",
			Path:           "/path1",
			Authorization:  "auth1",
		},
	}

	// 应该不会panic，但会记录错误日志
	CoverLocalConfig(fakeConf, cfgs)
}

// TestUpdateLocalConfig_NormalCase 测试UpdateLocalConfig正常情况
func TestUpdateLocalConfig_NormalCase(t *testing.T) {
	configer := NewLocalConfiger()

	// 初始配置
	initialCfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1",
			RemoteTarget:   "target1",
			Path:           "/path1",
			Authorization:  "auth1",
		},
	}

	// 更新配置
	updateCfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1_updated",
			RemoteTarget:   "target1_updated",
			Path:           "/path1_updated",
			Authorization:  "auth1_updated",
		},
		{
			Name:           "config2",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent2",
			RemoteTarget:   "target2",
			Path:           "/path2",
			Authorization:  "auth2",
		},
	}

	// 先添加初始配置
	UpdateLocalConfig(configer, initialCfgs)

	// 验证初始配置
	cfg1, ok := configer.GetProxyConfig("config1")
	assert.True(t, ok)
	assert.Equal(t, "agent1", cfg1.AgentID)

	// 执行更新操作
	UpdateLocalConfig(configer, updateCfgs)

	// 验证config1被更新
	cfg1Updated, ok := configer.GetProxyConfig("config1")
	assert.True(t, ok)
	assert.Equal(t, "agent1_updated", cfg1Updated.AgentID)

	// 验证config2被添加
	cfg2, ok := configer.GetProxyConfig("config2")
	assert.True(t, ok)
	assert.Equal(t, "agent2", cfg2.AgentID)
}

// TestUpdateLocalConfig_EmptyConfig 测试UpdateLocalConfig空配置情况
func TestUpdateLocalConfig_EmptyConfig(t *testing.T) {
	configer := NewLocalConfiger()

	// 空配置更新应该不会导致错误
	UpdateLocalConfig(configer, []ProxyConfig{})

	// 验证配置器为空
	cfg, ok := configer.GetProxyConfig("any")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

// TestDeleteLocalConfig_NormalCase 测试DeleteLocalConfig正常情况
func TestDeleteLocalConfig_NormalCase(t *testing.T) {
	configer := NewLocalConfiger()

	// 初始配置
	initialCfgs := []ProxyConfig{
		{
			Name:           "config1",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent1",
			RemoteTarget:   "target1",
			Path:           "/path1",
			Authorization:  "auth1",
		},
		{
			Name:           "config2",
			ProxyAgentCard: a2aserver.AgentCard{},
			AgentID:        "agent2",
			RemoteTarget:   "target2",
			Path:           "/path2",
			Authorization:  "auth2",
		},
	}

	// 添加配置
	UpdateLocalConfig(configer, initialCfgs)

	// 验证配置存在
	cfg1, ok := configer.GetProxyConfig("config1")
	assert.True(t, ok)
	assert.NotNil(t, cfg1)

	cfg2, ok := configer.GetProxyConfig("config2")
	assert.True(t, ok)
	assert.NotNil(t, cfg2)

	// 删除config1
	deleteCfgs := []ProxyConfig{
		{
			Name: "config1",
		},
	}

	DeleteLocalConfig(configer, deleteCfgs)

	// 验证config1被删除
	cfg1Deleted, ok := configer.GetProxyConfig("config1")
	assert.False(t, ok)
	assert.Nil(t, cfg1Deleted)

	// 验证config2仍然存在
	cfg2Remaining, ok := configer.GetProxyConfig("config2")
	assert.True(t, ok)
	assert.NotNil(t, cfg2Remaining)
}

// TestDeleteLocalConfig_NonExistentConfig 测试DeleteLocalConfig删除不存在的配置
func TestDeleteLocalConfig_NonExistentConfig(t *testing.T) {
	configer := NewLocalConfiger()

	// 尝试删除不存在的配置
	deleteCfgs := []ProxyConfig{
		{
			Name: "non_existent",
		},
	}

	// 应该不会panic
	DeleteLocalConfig(configer, deleteCfgs)

	// 验证配置器为空
	cfg, ok := configer.GetProxyConfig("non_existent")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

// TestDeleteLocalConfig_EmptyConfig 测试DeleteLocalConfig空配置情况
func TestDeleteLocalConfig_EmptyConfig(t *testing.T) {
	configer := NewLocalConfiger()

	// 空配置删除应该不会导致错误
	DeleteLocalConfig(configer, []ProxyConfig{})

	// 验证配置器为空
	cfg, ok := configer.GetProxyConfig("any")
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

// TestConcurrentAccess 测试并发访问安全性
func TestConcurrentAccess(t *testing.T) {
	configer := NewLocalConfiger()

	// 使用sync.Map，应该是线程安全的
	// 这里主要验证基本功能，详细的并发测试需要更复杂的场景
	cfg := ProxyConfig{
		Name:           "test",
		ProxyAgentCard: a2aserver.AgentCard{},
		AgentID:        "agent",
		RemoteTarget:   "target",
		Path:           "/path",
		Authorization:  "auth",
	}

	UpdateLocalConfig(configer, []ProxyConfig{cfg})

	result, ok := configer.GetProxyConfig("test")
	assert.True(t, ok)
	assert.Equal(t, "agent", result.AgentID)
}
