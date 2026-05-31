//go:build darwin || linux

package wecom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type fileProcessLock struct {
	file *os.File
}

func acquireProcessLock(path string) (processLock, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	if err := os.MkdirAll(
		filepath.Dir(path),
		sessionTrackerStoreDirPerm,
	); err != nil {
		return nil, fmt.Errorf(
			"wecom: create websocket lock dir: %w",
			err,
		)
	}

	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_RDWR,
		sessionTrackerStoreFilePerm,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wecom: open websocket lock: %w",
			err,
		)
	}

	err = syscall.Flock(
		int(file.Fd()),
		syscall.LOCK_EX|syscall.LOCK_NB,
	)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf(
			"wecom: acquire websocket lock %s: %w",
			path,
			err,
		)
	}

	return &fileProcessLock{file: file}, nil
}

func (l *fileProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	errUnlock := syscall.Flock(
		int(l.file.Fd()),
		syscall.LOCK_UN,
	)
	errClose := l.file.Close()
	l.file = nil

	if errUnlock != nil {
		return fmt.Errorf(
			"wecom: release websocket lock: %w",
			errUnlock,
		)
	}
	if errClose != nil {
		return fmt.Errorf(
			"wecom: close websocket lock: %w",
			errClose,
		)
	}
	return nil
}
