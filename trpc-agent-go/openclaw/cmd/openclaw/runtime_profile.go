package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/app"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/runtimeprofile"
)

type runtimeProfileProvider interface {
	RuntimeProfileOptions(
		ctx context.Context,
		paths startupPaths,
	) ([]app.RuntimeOption, error)
}

type runtimeProfileProviderFunc func(
	ctx context.Context,
	paths startupPaths,
) ([]app.RuntimeOption, error)

func (f runtimeProfileProviderFunc) RuntimeProfileOptions(
	ctx context.Context,
	paths startupPaths,
) ([]app.RuntimeOption, error) {
	if f == nil {
		return nil, nil
	}
	return f(ctx, paths)
}

var runtimeProfileProviders = struct {
	sync.RWMutex
	values []runtimeProfileProvider
}{}

func registerRuntimeProfileProvider(provider runtimeProfileProvider) {
	if provider == nil {
		return
	}
	runtimeProfileProviders.Lock()
	defer runtimeProfileProviders.Unlock()
	runtimeProfileProviders.values = append(
		runtimeProfileProviders.values,
		provider,
	)
}

func runtimeProfileOptions(
	ctx context.Context,
	paths startupPaths,
) ([]app.RuntimeOption, error) {
	opts, err := runtimeProfileOptionsFromConfig(ctx, paths)
	if err != nil {
		return nil, err
	}
	providers := runtimeProfileProviderSnapshot()
	for i, provider := range providers {
		current, err := provider.RuntimeProfileOptions(ctx, paths)
		if err != nil {
			return nil, fmt.Errorf(
				"runtime profile provider %d: %w",
				i+1,
				err,
			)
		}
		opts = appendRuntimeProfileOptions(opts, current...)
	}
	return opts, nil
}

type runtimeProfileFileConfig struct {
	RuntimeProfiles *runtimeprofile.Config `yaml:"runtime_profiles,omitempty"`
}

func runtimeProfileOptionsFromConfig(
	ctx context.Context,
	paths startupPaths,
) ([]app.RuntimeOption, error) {
	_ = ctx
	cfg, ok, err := loadRuntimeProfileSelectorConfig(
		paths.OpenClawConfigPath,
	)
	if err != nil || !ok {
		return nil, err
	}
	resolver, catalog, required, err := runtimeProfileResolverFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return []app.RuntimeOption{
		app.WithRuntimeProfileResolver(resolver, required),
		app.WithRuntimeProfileCatalog(catalog),
	}, nil
}

func loadRuntimeProfileSelectorConfig(
	path string,
) (runtimeprofile.Config, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return runtimeprofile.Config{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return runtimeprofile.Config{}, false, fmt.Errorf(
			"read runtime profile config: %w",
			err,
		)
	}
	var file runtimeProfileFileConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return runtimeprofile.Config{}, false, fmt.Errorf(
			"parse runtime profile config: %w",
			err,
		)
	}
	if file.RuntimeProfiles == nil ||
		len(file.RuntimeProfiles.Selectors) == 0 {
		return runtimeprofile.Config{}, false, nil
	}
	return *file.RuntimeProfiles, true, nil
}

func runtimeProfileResolverFromConfig(
	cfg runtimeprofile.Config,
) (
	runtimeprofile.Resolver,
	runtimeprofile.Catalog,
	bool,
	error,
) {
	if err := runtimeprofile.ValidateConfig(cfg); err != nil {
		return nil, nil, false, err
	}
	store := runtimeprofile.StaticStore{Config: cfg}
	cached := runtimeprofile.NewCachedResolver(store)
	return cached, cached, true, nil
}

func runtimeProfileProviderSnapshot() []runtimeProfileProvider {
	runtimeProfileProviders.RLock()
	defer runtimeProfileProviders.RUnlock()
	if len(runtimeProfileProviders.values) == 0 {
		return nil
	}
	return append(
		[]runtimeProfileProvider(nil),
		runtimeProfileProviders.values...,
	)
}

func appendRuntimeProfileOptions(
	base []app.RuntimeOption,
	extra ...app.RuntimeOption,
) []app.RuntimeOption {
	for _, opt := range extra {
		if opt == nil {
			continue
		}
		base = append(base, opt)
	}
	return base
}

func resetRuntimeProfileProvidersForTest() func() {
	runtimeProfileProviders.Lock()
	old := append(
		[]runtimeProfileProvider(nil),
		runtimeProfileProviders.values...,
	)
	runtimeProfileProviders.values = nil
	runtimeProfileProviders.Unlock()
	return func() {
		runtimeProfileProviders.Lock()
		defer runtimeProfileProviders.Unlock()
		runtimeProfileProviders.values = old
	}
}
