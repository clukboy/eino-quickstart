package restapi

import (
	"testing"
	"time"
)

// TestStopWithoutStartDoesNotBlock 覆盖一个会让整个进程挂住的边界。
//
// ServiceGroup 的 Stop 会并发调用每个 service 的 Stop 并等它们全部返回，所以
// 一个永远阻塞的 Stop 等于关不掉进程。Start 没跑过时（配置错、端口被占，
// 于是在建监听器之前就返回了）Stop 必须立刻返回。
func TestStopWithoutStartDoesNotBlock(t *testing.T) {
	server := &Server{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Stop()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop blocked even though Start never ran")
	}
}
