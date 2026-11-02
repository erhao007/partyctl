package service

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalContext 在 SIGINT/SIGTERM 时取消，供监视进程退出。
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
