package main

import (
	"fmt"

	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

// serviceCommand — установка и управление службой.
//
// Все эти действия требуют прав администратора: ребёнок не должен уметь ни
// поставить службу, ни снять её.
func serviceCommand(sub string) error {
	switch sub {
	case "install":
		if err := winsvc.Install(); err != nil {
			return err
		}
		fmt.Printf("служба %q установлена, автозапуск включён\n", winsvc.Name)
		fmt.Println("запустить: mykids-agent service start")
		fmt.Println("не забудьте поставить помощника в автозапуск пользователя: mykids-agent helper")
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
