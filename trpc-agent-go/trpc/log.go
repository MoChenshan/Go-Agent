package trpc

import (
	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

func init() {
	tlog.DefaultLogger.SetLevel("0", tlog.LevelInfo)
	log.Default = tlog.DefaultLogger

	log.DebugContext = tlog.DebugContext
	log.DebugfContext = tlog.DebugContextf
	log.InfoContext = tlog.InfoContext
	log.InfofContext = tlog.InfoContextf
	log.WarnContext = tlog.WarnContext
	log.WarnfContext = tlog.WarnContextf
	log.ErrorContext = tlog.ErrorContext
	log.ErrorfContext = tlog.ErrorContextf
	log.FatalContext = tlog.FatalContext
	log.FatalfContext = tlog.FatalContextf

	pluginKey := "log-default"
	hook := plugin.GetSetupHook(pluginKey)
	plugin.RegisterSetupHook(pluginKey, func(setup func() error) error {
		if err := hook(setup); err != nil {
			return err
		}
		log.Default = tlog.DefaultLogger
		log.DebugContext = tlog.DebugContext
		log.DebugfContext = tlog.DebugContextf
		log.InfoContext = tlog.InfoContext
		log.InfofContext = tlog.InfoContextf
		log.WarnContext = tlog.WarnContext
		log.WarnfContext = tlog.WarnContextf
		log.ErrorContext = tlog.ErrorContext
		log.ErrorfContext = tlog.ErrorContextf
		log.FatalContext = tlog.FatalContext
		log.FatalfContext = tlog.FatalContextf
		return nil
	})
}
