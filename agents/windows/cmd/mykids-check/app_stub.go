//go:build !windows

package main

import (
	"fmt"
	"os"
)

// Вне Windows проверять нечего: всё, что она смотрит, существует только там.
// Собирается на любой ОС, чтобы `go build ./...` и `go vet ./...` проходили
// везде, — иначе ошибку в этой команде увидел бы только релизный прогон.
func run() error {
	return fmt.Errorf("проверка имеет смысл только в Windows")
}

func fail(text string) {
	fmt.Fprintln(os.Stderr, text)
	os.Exit(1)
}
