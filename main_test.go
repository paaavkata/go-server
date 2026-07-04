package goserver

import (
	"os"
	"testing"

	logger "github.com/paaavkata/go-logger"
)

// TestMain initializes go-logger before any test runs. In production each
// service calls logger.Init() in main(); the Manager assumes an initialized
// logger, so the test package must do the same.
func TestMain(m *testing.M) {
	logger.Init("info", "plain", "go-server-test", "test", false, true, false, nil, nil)
	os.Exit(m.Run())
}
