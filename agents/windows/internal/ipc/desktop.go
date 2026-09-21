package ipc

import (
	"sync"
	"time"
)

// SilentProcess — что служба считает активным окном, пока помощник молчит.
//
// Имя заведомо не совпадает ни с одним исполняемым файлом, поэтому в белый
// список не попадёт и экран будет закрыт. Так и задумано: не видя рабочего
// стола, разрешать им пользоваться нельзя.
const SilentProcess = "(помощник молчит)"

// DefaultStale — после какого молчания помощник считается пропавшим.
// Помощник шлёт наблюдения каждые несколько секунд, так что полминуты — это
// уже не задержка, а отсутствие.
const DefaultStale = 30 * time.Second

// Desktop — рабочий стол глазами службы: последнее, что сообщил помощник.
//
// Пока помощник молчит, служба считает, что экраном пользуются: активное окно
// неизвестно, простоя нет. Это та же асимметрия, что и во всём остальном —
// снять наблюдателя не должно быть способом получить бесплатное время. Цена
// ошибки несимметрична: лишняя минута списания против часов даром.
//
// Безопасен для одновременного доступа: наблюдения кладёт поток соединения,
// а читает их цикл учёта.
type Desktop struct {
	mu   sync.Mutex
	last Sample
	// seen — когда наблюдение пришло, по часам службы. Часам помощника здесь
	// верить нельзя: он работает с правами ребёнка.
	seen  time.Time
	stale time.Duration
	now   func() time.Time
	// own — собственный источник состояния экрана. Может быть пустым: тогда
	// о блокировке известно только со слов помощника.
	own LockSource
}

// NewDesktop создаёт источник наблюдений. Нулевой stale означает стандартный.
func NewDesktop(stale time.Duration, now func() time.Time) *Desktop {
	if stale <= 0 {
		stale = DefaultStale
	}
	if now == nil {
		now = time.Now
	}
	return &Desktop{stale: stale, now: now}
}

// SetLockSource подключает собственный источник состояния экрана.
//
// Отдельным вызовом, а не полем конструктора: источник платформенный, а
// Desktop нужен и там, где его нет вовсе.
func (d *Desktop) SetLockSource(own LockSource) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.own = own
}

// ownLock спрашивает собственный источник.
//
// Вызов наружу делается без захваченного замка: под ним сидят и цикл учёта, и
// поток соединения, а поход в Windows — хоть и быстрый, но всё-таки поход.
func (d *Desktop) ownLock() LockState {
	d.mu.Lock()
	own := d.own
	d.mu.Unlock()
	if own == nil {
		return LockUnknown
	}
	return own.SessionLock()
}

// Update принимает наблюдение от помощника.
func (d *Desktop) Update(s Sample) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.last, d.seen = s, d.now()
}

// Silent сообщает, что помощника давно не слышно.
func (d *Desktop) Silent() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.silent()
}

// Since — сколько прошло с последнего наблюдения. Ноль, если их не было вовсе.
func (d *Desktop) Since() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen.IsZero() {
		return 0
	}
	return d.now().Sub(d.seen)
}

func (d *Desktop) silent() bool {
	return d.seen.IsZero() || d.now().Sub(d.seen) > d.stale
}

// ForegroundProcess — имя исполняемого файла активного окна.
func (d *Desktop) ForegroundProcess() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.silent() {
		return SilentProcess, nil
	}
	return d.last.Process, nil
}

// IdleTime — сколько прошло с последнего ввода.
//
// Молчание помощника даёт ноль, а не «давно»: иначе его достаточно было бы
// убить, чтобы время перестало списываться.
func (d *Desktop) IdleTime() (time.Duration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.silent() {
		return 0, nil
	}
	return time.Duration(d.last.IdleSeconds) * time.Second, nil
}

// SessionLocked сообщает, заблокирован ли экран.
//
// Свой источник главнее помощника, и главнее в обе стороны. Он не зависит от
// помощника вовсе: подменить ответ Windows о состоянии сессии из-под учётной
// записи ребёнка нельзя, а заблокировать экран и одновременно им пользоваться
// нельзя тем более.
//
// Поэтому его «заблокирован» принимается и тогда, когда помощник молчит.
// Правило «молчание — это расход» осталось правилом для всего, о чём спросить
// нельзя: активного окна и простоя. О блокировке спросить можно.
//
// Если своего источника нет или он не ответил — всё как раньше: слово
// помощника, а его молчание даёт «не заблокирован», потому что соврать об
// этом было бы выгодно.
func (d *Desktop) SessionLocked() bool {
	switch d.ownLock() {
	case LockOn:
		return true
	case LockOff:
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.silent() {
		return false
	}
	return d.last.SessionLocked
}
