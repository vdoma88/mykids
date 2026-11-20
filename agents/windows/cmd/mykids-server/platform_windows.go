//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"github.com/vdoma88/mykids/agents/windows/internal/serverctl"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

func serviceCommand(sub string, o options) error {
	switch sub {
	case "install":
		if _, installed, err := winsvc.State(); err != nil {
			return err
		} else if installed {
			if err := winsvc.Uninstall(); err != nil {
				return err
			}
		}
		return winsvc.Install("-home", o.L.Home, "-data", o.L.Data, "serve")
	case "uninstall":
		if _, installed, err := winsvc.State(); err != nil || !installed {
			return err
		}
		return winsvc.Uninstall()
	case "start":
		return winsvc.Start()
	case "stop":
		if state, installed, err := winsvc.State(); err != nil || !installed || state == svc.Stopped {
			return err
		}
		return winsvc.Control(svc.Stop, svc.Stopped)
	}
	return fmt.Errorf("неизвестное действие %q", sub)
}

// stopped — служба есть и остановлена: ждать её ответа дальше бессмысленно.
func stopped() bool {
	state, installed, err := winsvc.State()
	return err == nil && installed && state == svc.Stopped
}

// firewallRule — имя правила. Латиницей: netsh в русской Windows
// показывает имена правил в кодировке консоли, и кириллица там ломается.
const firewallRule = "MyKids Server"

// firewall открывает порт MyKids для домашней сети.
//
// Только для своей подсети (remoteip=localsubnet), но во всех профилях: в
// Windows 11 домашнюю сеть часто помечают «общественной», и правило только
// для «частных» сетей молча не сработало бы — компьютер ребёнка не увидел бы
// сервер, и искать причину родитель стал бы где угодно, кроме брандмауэра.
func firewall(sub string, o options) error {
	switch sub {
	case "add":
		cfg, err := serverctl.LoadConfig(o.L.Config())
		if err != nil {
			return err
		}
		_ = netsh("advfirewall", "firewall", "delete", "rule", "name="+firewallRule)
		return netsh("advfirewall", "firewall", "add", "rule", "name="+firewallRule,
			"dir=in", "action=allow", "protocol=TCP", "localport="+strconv.Itoa(cfg.Port),
			"remoteip=localsubnet", "profile=any",
			"description=MyKids: admin and child pages for devices at home")
	case "remove":
		_ = netsh("advfirewall", "firewall", "delete", "rule", "name="+firewallRule)
		return nil
	}
	return fmt.Errorf("неизвестное действие %q", sub)
}

func netsh(args ...string) error {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("netsh: %v %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}
