//go:build !darwin && !linux

package wecom

func acquireProcessLock(_ string) (processLock, error) {
	return nil, nil
}
