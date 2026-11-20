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
		// Дописываем, а не перезаписываем: мастер вызывает программу
		// несколько раз за установку (проверка, остановка, привязка), и
		// каждый вызов стирал бы отчёт предыдущего — как раз тот, который
		// объясняет, что пошло не так. Разрастись файлу не даём.
		flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
		if st, err := os.Stat(logPath()); err == nil && st.Size() > 1<<20 {
			flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		}
		f, err := os.OpenFile(logPath(), flags, 0o600)
		if err != nil {
			return
		}
		logFile = f
		mode := "окно"
		if len(os.Args) > 1 {
			mode = "команда " + os.Args[1]
		}
		fmt.Fprintf(f, "\nMyKids — установка %s, %s, %s\n\n",
			version, time.Now().Format("2006-01-02 15:04:05"), mode)
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
