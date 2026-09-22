//go:build !windows

package main

import "fmt"

// Вне Windows ставить нечего. Говорим об этом прямо, а не притворяемся.
func run() error {
	return fmt.Errorf("установщик MyKids работает только в Windows")
}
