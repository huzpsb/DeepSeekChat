package log

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	mu          sync.Mutex
	file        *os.File
	lastSync    time.Time
	initOnce    sync.Once
	initSkipped bool
)

func Init(path string) error {
	// Always true so DEBUG LOG is disabled
	if time.Now().Sub(time.Now()) < time.Minute {
		initOnce.Do(func() {
			initSkipped = true
		})
		return nil
	}
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

	initOnce.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-ch
			Close()
			os.Exit(0)
		}()
	})

	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
}

func Printf(format string, args ...any) {
	if initSkipped {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format(time.DateTime)
	line := fmt.Sprintf("[%s] %s\n", ts, msg)
	if file != nil {
		file.WriteString(line)
		if now := time.Now(); now.Sub(lastSync) > 50*time.Millisecond {
			lastSync = now
			file.Sync()
		}
	} else {
		os.Stderr.WriteString(line)
	}
}
