package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

const (
	serviceNameSystemd = "timetracker"
	serviceNameLaunchd = "com.timetracker.daemon"
)

type Kind int

const (
	KindNone Kind = iota
	KindSystemd
	KindLaunchd
)

func Detect() Kind {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("launchctl"); err == nil {
			return KindLaunchd
		}
	case "linux":
		if systemdAvailable() {
			return KindSystemd
		}
	}
	return KindNone
}

func InstallDaemon(binaryPath string) error {
	switch Detect() {
	case KindSystemd:
		return installSystemd(binaryPath)
	case KindLaunchd:
		return installLaunchd(binaryPath)
	default:
		fmt.Println("  note: no supported service manager found (systemd/launchd)")
		fmt.Println("        idle auto-stop will use the heartbeat fallback instead")
		return nil
	}
}

func UninstallDaemon() error {
	switch Detect() {
	case KindSystemd:
		return uninstallSystemd()
	case KindLaunchd:
		return uninstallLaunchd()
	default:
		return nil
	}
}

func DaemonInstalled() bool {
	switch Detect() {
	case KindSystemd:
		return serviceFilePathSystemd() != "" &&
			fileExists(serviceFilePathSystemd())
	case KindLaunchd:
		p, _ := serviceFilePathLaunchd()
		return fileExists(p)
	default:
		return false
	}
}

var systemdUnitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=Timetracker idle watchdog
After=default.target

[Service]
ExecStart={{.Binary}} daemon
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
`))

func installSystemd(binaryPath string) error {
	unitPath := serviceFilePathSystemd()
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("mkdir systemd dir: %w", err)
	}

	f, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("create unit file: %w", err)
	}
	defer f.Close()

	if err := systemdUnitTmpl.Execute(f, struct{ Binary string }{binaryPath}); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", serviceNameSystemd},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	fmt.Printf("  installed systemd user service: %s\n", unitPath)
	return nil
}

func uninstallSystemd() error {
	for _, args := range [][]string{
		{"--user", "disable", "--now", serviceNameSystemd},
	} {
		// Ignore errors — service may not be running.
		exec.Command("systemctl", args...).Run() //nolint:errcheck
	}
	unitPath := serviceFilePathSystemd()
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run() //nolint:errcheck
	fmt.Printf("  removed systemd user service\n")
	return nil
}

func serviceFilePathSystemd() string {
	cfgHome := os.Getenv("XDG_CONFIG_HOME")
	if cfgHome == "" {
		cfgHome = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(cfgHome, "systemd", "user", serviceNameSystemd+".service")
}

func systemdAvailable() bool {
	out, err := exec.Command("systemctl", "--user", "status").CombinedOutput()
	if err == nil {
		return true
	}
	output := string(out)
	unavailable := []string{
		"transport endpoint is not connected",
		"Failed to connect to bus",
		"No such file or directory",
	}
	for _, s := range unavailable {
		if strings.Contains(output, s) {
			return false
		}
	}
	return strings.Contains(output, "State:") || strings.Contains(output, "running")
}

var launchdPlistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{.Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{.Binary}}</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>{{.LogDir}}/timetracker.log</string>
  <key>StandardErrorPath</key>
  <string>{{.LogDir}}/timetracker.log</string>
</dict>
</plist>
`))

func installLaunchd(binaryPath string) error {
	plistPath, err := serviceFilePathLaunchd()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}

	logDir := filepath.Join(os.Getenv("HOME"), "Library", "Logs")

	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer f.Close()

	data := struct {
		Label, Binary, LogDir string
	}{serviceNameLaunchd, binaryPath, logDir}
	if err := launchdPlistTmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w\n%s", err, out)
	}
	fmt.Printf("  installed launchd agent: %s\n", plistPath)
	return nil
}

func uninstallLaunchd() error {
	plistPath, err := serviceFilePathLaunchd()
	if err != nil {
		return err
	}
	// Ignore errors — agent may not be loaded.
	exec.Command("launchctl", "unload", plistPath).Run() //nolint:errcheck
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Println("  removed launchd agent")
	return nil
}

func serviceFilePathLaunchd() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("$HOME not set")
	}
	return filepath.Join(home, "Library", "LaunchAgents", serviceNameLaunchd+".plist"), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
