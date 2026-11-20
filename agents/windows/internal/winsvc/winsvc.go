// Package winsvc — обвязка службы Windows.
//
// Тонкая намеренно: всё, что служба делает, живёт в internal/service и
// проверяется тестами на любой ОС. Здесь только то, что нельзя ни проверить,
// ни выполнить вне Windows, — разговор с диспетчером служб.
package winsvc

import "time"

// Имя, подпись и описание службы. Переменные, а не константы: этой же
// обвязкой пользуется служба сервера, и она называет себя сама (Configure).

// Name — имя службы в системе.
var Name = "MyKidsAgent"

// DisplayName — как её видно в оснастке «Службы».
var DisplayName = "MyKids — контроль экранного времени"

// Description — описание там же. Родитель должен понимать, что это, спустя
// полгода после установки.
var Description = "Считает экранное время ребёнка и закрывает экран, " +
	"когда оно кончилось. Остановка службы фиксируется и оплачивается."

// Account — под кем работает служба. Пусто — LocalSystem.
var Account = ""

// StopWait — сколько ждать, пока работа доделает своё при остановке.
var StopWait = 20 * time.Second

// Configure называет службу. Вызывается в начале main, до всего остального.
func Configure(name, display, description string, stopWait time.Duration) {
	Name, DisplayName, Description, StopWait = name, display, description, stopWait
}
