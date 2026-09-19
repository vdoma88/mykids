//go:build windows

package ipc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// DefaultAddr — именованный канал, на котором служба ждёт помощника.
const DefaultAddr = `\\.\pipe\mykids-agent`

// pipeSDDL — кто может открыть канал.
//
//	SY — сама служба (LocalSystem), полный доступ
//	BA — администраторы, полный доступ: иначе родитель не продиагностирует
//	IU — вошедший в систему пользователь, чтение и запись
//
// Помощник работает с правами ребёнка, поэтому доступ ему открыть обязаны.
// Это и есть предел того, что даёт разграничение: подсунуть свой помощник
// ребёнок технически может. Защищает не оно, а то, что молчание и неправдоподобные
// наблюдения служба считает расходом, а не отдыхом.
const pipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"

// Listen открывает канал для помощника.
func Listen(addr string) (net.Listener, error) {
	return winio.ListenPipe(addr, &winio.PipeConfig{
		SecurityDescriptor: pipeSDDL,
		InputBufferSize:    MaxLine,
		OutputBufferSize:   MaxLine,
	})
}

// DialAddr подключается к службе.
func DialAddr(addr string) (net.Conn, error) {
	timeout := 5 * time.Second
	return winio.DialPipe(addr, &timeout)
}
