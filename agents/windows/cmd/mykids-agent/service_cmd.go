package main

import (
	"fmt"

	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

// serviceCommand — установка и управление службой.
//
// Все эти действия требуют прав администратора: ребёнок не должен уметь ни
// поставить службу, ни снять её.
func serviceCommand(sub string, o options) error {
	switch sub {
	case "install":
		// Каталог данных записываем в командную строку службы. Без этого
		// служба взяла бы свой по умолчанию, и установка с другим -data
		// привела бы к тому, что привязка лежит в одном месте, а служба
		// ищет её в другом — и молча работает автономно.
		if err := winsvc.Install("-data", o.dataDir, "-pipe", o.pipe, "serve"); err != nil {
			return err
		}
		fmt.Printf("служба %q установлена, автозапуск включён\n", winsvc.Name)
		fmt.Printf("каталог данных: %s\n", o.dataDir)
		fmt.Println("запустить: mykids-agent service start")
		// Помощника служба поднимает сама: он живёт в сессии ребёнка, а
		// автозапуск в его профиле ребёнок же и отключил бы.
		return nil

	case "uninstall":
		if err := winsvc.Uninstall(); err != nil {
			return err
		}
		fmt.Printf("служба %q удалена\n", winsvc.Name)
		return nil

	case "start":
		if err := winsvc.Start(); err != nil {
			return err
		}
		fmt.Println("служба запущена")
		return nil

	case "stop":
		if err := stopService(); err != nil {
			return err
		}
		fmt.Println("служба остановлена")
		return nil

	case "status":
		state, err := winsvc.Query()
		if err != nil {
			return err
		}
		fmt.Printf("служба %q: %s\n", winsvc.Name, state)
		return nil

	case "":
		return fmt.Errorf("укажите действие: install, uninstall, start, stop или status")
	default:
		return fmt.Errorf("неизвестное действие %q", sub)
	}
}
