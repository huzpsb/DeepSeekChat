package log

import (
	"fmt"
	"internal/runtime/exithook"
	"os"
	"sync"
	"time"
)

var (
	mu       sync.Mutex
	file     *os.File
	lastSync time.Time
)

func Init(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	file = f

	exithook.Add(exithook.Hook{
		F: func() {
			mu.Lock()
			defer mu.Unlock()
			file.Close()
		},
		RunOnFailure: true,
	})

	return nil

}

func Printf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format(time.DateTime)
	line := fmt.Sprintf("[%s] %s\n", ts, msg)
	if file != nil {
		file.WriteString(line)
		now := time.Now()
		if now.Sub(lastSync) > time.Second {
			lastSync = now
			file.Sync()
		}
	} else {
		os.Stderr.WriteString(line)
	}
}
