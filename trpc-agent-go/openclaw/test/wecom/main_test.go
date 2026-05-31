package wecome2e_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedHarness != nil {
		sharedHarness.close()
	}
	os.Exit(code)
}
