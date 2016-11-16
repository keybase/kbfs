package libkbfs

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	result := m.Run()
	numGoroutines := runtime.NumGoroutine()
	fmt.Printf("NumGoroutine(): %d\n", numGoroutines)
	sleep := 10 * time.Second
	time.Sleep(sleep)
	numGoroutines = runtime.NumGoroutine()
	fmt.Printf("NumGoroutine() after %v: %d\n", sleep, numGoroutines)
	buf := make([]byte, 1024*1024)
	stackLen := runtime.Stack(buf, true)
	fmt.Printf("Stack (len %d):\n", stackLen)
	fmt.Println(string(buf))
	os.Exit(result)
}
