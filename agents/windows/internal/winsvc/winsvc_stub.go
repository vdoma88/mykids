//go:build !windows

package winsvc

import (
	"context"
	"errors"
)

// ErrNotWindows — службы есть только в Windows. Возвращаем понятную ошибку,
// а не молчаливое «получилось»: иначе установка на другой ОС выглядела бы
// удачной и обнаружилась бы только на компьютере ребёнка.
var ErrNotWindows = errors.New("служба доступна только в Windows")

// Body — работа службы.
type Body func(ctx context.Context, log func(string, ...any)) error

func InService() bool        { return false }
func Run(Body) error         { return ErrNotWindows }
func Install() error         { return ErrNotWindows }
func Uninstall() error       { return ErrNotWindows }
func Start() error           { return ErrNotWindows }
func Query() (string, error) { return "", ErrNotWindows }
