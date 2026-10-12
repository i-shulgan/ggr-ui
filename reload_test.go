//go:build unix

package main

import (
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestReloadOnSIGHUP(t *testing.T) {
	var calls atomic.Int32
	s := countingSelenoid(t, 1, &calls, false)
	dir := t.TempDir()
	writeQuota(t, dir, "alice", s)
	useQuotaDir(t, dir)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		reloadOnSignal(sig)
		close(done)
	}()
	defer func() {
		signal.Stop(sig)
		close(sig)
		<-done
	}()

	writeQuota(t, dir, "bob", s)
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		lock.RLock()
		_, ok := hosts["bob"]
		lock.RUnlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("quota was not reloaded on SIGHUP")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
