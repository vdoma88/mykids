//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Отчёт пишется не только в окно, но и в файл.
//
// Это не перестраховка. Установка идёт один раз, в чужой учётной записи, и
// если окно закроется само, то родитель останется вообще без объяснений — а
// объяснять придётся именно то, что произошло в последние секунды. У
// программы, которая ставит систему, собственный сбой скрыть легче всего.

var (
	logOnce sync.Once
	logFile *os.File
	logMu   sync.Mutex
)

func logPath() string {
	dir := os.Getenv("TEMP")
	if dir == "" {
		dir = os.Getenv("TMP")
	}
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "mykids-setup.log")
}

func toFile(line string) {
	logOnce.Do(func() {
		f, err := os.Create(logPath())
		if err != nil {
			return
		}
		logFile = f
		fmt.Fprintf(f, "MyKids — установка %s, %s\n\n",
			version, time.Now().Format("2006-01-02 15:04:05"))
	})
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		return
	}
	fmt.Fprintln(logFile, line)
	logFile.Sync()
}

// alsoToFile дублирует в файл всё, что обработчик пишет в окно.
func alsoToFile(do func(f form, log func(string))) func(f form, log func(string)) {
	return func(f form, log func(string)) {
		do(f, func(line string) {
			toFile(line)
			log(line)
		})
	}
}
