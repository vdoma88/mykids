// Package ipc — разговор службы с помощником в сессии пользователя.
//
// Разделение не по вкусу, а по устройству Windows: служба под LocalSystem
// живёт в нулевой сессии и рабочего стола пользователя не видит совсем. Кто
// сейчас в активном окне и давно ли не было ввода, знает только процесс внутри
// сессии. Значит, наблюдает помощник, а решает служба — её ребёнок остановить
// не может.
//
// Формат — строки JSON, запрос и ответ строго по очереди. Транспорт задаётся
// снаружи: под Windows это именованный канал с проверкой SID вызывающего, а в
// тестах — обычная пара в памяти. Протокол от транспорта не зависит, и это
// позволяет проверить его целиком там, где Windows нет.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/screen"
)

// MaxLine — предел длины строки. Помощник работает с правами ребёнка, и
// подсунуть службе бесконечную строку не должно быть способом её положить.
const MaxLine = 64 * 1024

// Sample — что помощник видит на рабочем столе.
//
// Это наблюдения, а не решения: помощнику не доверяют ни остаток времени, ни
// политику. Всё, что он сообщает, служба перепроверяет своим учётом.
type Sample struct {
	At      time.Time `json:"at"`
	Process string    `json:"process"`
	// Сколько прошло с последнего ввода.
	IdleSeconds int `json:"idleSeconds"`
	// Экран заблокирован или пользователь вышел.
	SessionLocked bool `json:"sessionLocked"`
}

// Verdict — что служба решила и что помощник должен показать.
type Verdict struct {
	Allow    bool          `json:"allow"`
	Reason   string        `json:"reason,omitempty"`
	Window   string        `json:"window,omitempty"`
	LeftSecs int           `json:"leftSecs"`
	WarnSoon bool          `json:"warnSoon"`
	Screen   screen.Screen `json:"screen,omitempty"`
	// Tray — подпись и меню значка. Готовит их служба по той же причине, что и
	// текст экрана: помощник работает с правами ребёнка, и сочинять надписи
	// ему не положено.
	Tray screen.Tray `json:"tray,omitempty"`
}

// ErrClosed — собеседник закрыл соединение. Не ошибка: помощник перезапускается,
// служба живёт дальше.
var ErrClosed = errors.New("соединение закрыто")

// Handler превращает наблюдение в решение. Его реализует служба.
type Handler func(Sample) Verdict

// Serve обслуживает одно соединение с помощником до его закрытия.
//
// Ошибку разбора не глотаем и соединение закрываем: помощник, который шлёт
// мусор, — либо сломан, либо подменён, и в обоих случаях разговаривать с ним
// дальше нельзя.
func Serve(rw io.ReadWriteCloser, h Handler) error {
	defer rw.Close()

	reader := bufio.NewReaderSize(rw, MaxLine)
	encoder := json.NewEncoder(rw)

	for {
		line, err := readLine(reader)
		if err != nil {
			return err
		}

		var s Sample
		if err := json.Unmarshal(line, &s); err != nil {
			return fmt.Errorf("разбор наблюдения: %w", err)
		}
		if err := encoder.Encode(h(s)); err != nil {
			return fmt.Errorf("отправка решения: %w", err)
		}
	}
}

// Conn — сторона помощника.
type Conn struct {
	rw      io.ReadWriteCloser
	reader  *bufio.Reader
	encoder *json.Encoder
}

// Dial оборачивает уже открытый транспорт.
func Dial(rw io.ReadWriteCloser) *Conn {
	return &Conn{rw: rw, reader: bufio.NewReaderSize(rw, MaxLine), encoder: json.NewEncoder(rw)}
}

// Close закрывает соединение.
func (c *Conn) Close() error { return c.rw.Close() }

// Exchange отдаёт наблюдение и ждёт решения.
func (c *Conn) Exchange(s Sample) (Verdict, error) {
	if err := c.encoder.Encode(s); err != nil {
		return Verdict{}, fmt.Errorf("отправка наблюдения: %w", err)
	}
	line, err := readLine(c.reader)
	if err != nil {
		return Verdict{}, err
	}
	var v Verdict
	if err := json.Unmarshal(line, &v); err != nil {
		return Verdict{}, fmt.Errorf("разбор решения: %w", err)
	}
	return v, nil
}

// readLine читает строку, не давая собеседнику съесть память.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	switch {
	case errors.Is(err, io.EOF) && len(line) == 0:
		return nil, ErrClosed
	case errors.Is(err, bufio.ErrBufferFull):
		return nil, fmt.Errorf("строка длиннее %d байт", MaxLine)
	case err != nil && !errors.Is(err, io.EOF):
		return nil, err
	}
	return line, nil
}
