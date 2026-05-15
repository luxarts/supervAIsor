package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	logFileName  = "poller.log"
	serviceLabel = "com.supervaisor.poller"
	// daemonEnvFlag tags the child process so subcommand dispatch can be
	// short-circuited if the binary ever re-invokes itself.
	daemonEnvFlag = "SUPERVAISOR_DAEMON"
)

func logPath(home string) string { return filepath.Join(home, settingsDir, logFileName) }

func usage() string {
	return strings.TrimSpace(`
Usage: supervaisor <command>

Commands:
  install     Install the background service (launchd on macOS, systemd --user on Linux)
  uninstall   Stop the service, remove the unit file, the binary, and ~/.supervaisor/
  start       Start the installed service
  stop        Stop the service
  restart     Restart the service
  status      Report service state
  run         Run the poller in the foreground (default when no args)

With no arguments the poller runs in the foreground — use that mode for
debugging. For long-running setups, install it as a service.`)
}

func dispatch(args []string, home string) bool {
	if os.Getenv(daemonEnvFlag) == "1" || len(args) == 0 {
		return false
	}
	switch args[0] {
	case "run":
		return false
	case "install":
		mustOK(cmdInstall(home))
	case "uninstall":
		mustOK(cmdUninstall(home))
	case "start":
		mustOK(serviceCmd("start"))
	case "stop":
		mustOK(serviceCmd("stop"))
	case "restart":
		mustOK(serviceCmd("restart"))
	case "status":
		mustOK(cmdStatus())
	case "-h", "--help", "help":
		fmt.Println(usage())
	default:
		fmt.Fprintln(os.Stderr, usage())
		os.Exit(2)
	}
	return true
}

func mustOK(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "supervaisor: %v\n", err)
	os.Exit(1)
}

// service abstracts the OS-native background-service backend.
type service interface {
	install(home, exe, log string) error
	uninstall(home string) error
	start() error
	stop() error
	restart() error
	status() (state string, err error)
}

func newService() (service, error) {
	switch runtime.GOOS {
	case "darwin":
		return &launchd{}, nil
	case "linux":
		return &systemdUser{}, nil
	default:
		return nil, fmt.Errorf("unsupported OS for service manager: %s", runtime.GOOS)
	}
}

func serviceCmd(verb string) error {
	svc, err := newService()
	if err != nil {
		return err
	}
	switch verb {
	case "start":
		return svc.start()
	case "stop":
		return svc.stop()
	case "restart":
		return svc.restart()
	}
	return fmt.Errorf("unknown verb: %s", verb)
}

func cmdInstall(home string) error {
	svc, err := newService()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if err := os.MkdirAll(filepath.Join(home, settingsDir), 0o755); err != nil {
		return err
	}
	if err := svc.install(home, exe, logPath(home)); err != nil {
		return err
	}
	fmt.Printf("supervaisor: service installed and started (logs: %s)\n", logPath(home))
	return nil
}

func cmdStatus() error {
	svc, err := newService()
	if err != nil {
		return err
	}
	state, err := svc.status()
	if err != nil {
		return err
	}
	fmt.Printf("supervaisor: %s\n", state)
	return nil
}

func cmdUninstall(home string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Join(home, settingsDir)
	fmt.Printf("This will remove:\n  service unit\n  %s\n  %s\nContinue? [y/N]: ", exe, dir)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	if !strings.EqualFold(strings.TrimSpace(line), "y") {
		fmt.Println("supervaisor: aborted")
		return nil
	}
	// Best-effort: tolerate "not installed" so uninstall is idempotent.
	if svc, err := newService(); err == nil {
		_ = svc.uninstall(home)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if err := os.Remove(exe); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove binary: %w", err)
	}
	fmt.Println("supervaisor: uninstalled")
	return nil
}

// ---------- launchd (macOS) ----------

type launchd struct{}

func launchdPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
}

func launchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func launchdTarget() string {
	return launchdDomain() + "/" + serviceLabel
}

// xmlEscape covers the few characters that can appear in expanded paths.
// Plist <string> bodies are XML, so unescaped '&' or '<' would break parsing.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func (l *launchd) install(home, exe, log string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTD/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>%s</string>
  </dict>
</dict>
</plist>
`, serviceLabel, xmlEscape(exe), xmlEscape(log), xmlEscape(log), xmlEscape(home))

	path := launchdPlistPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	// Bootout first to make install idempotent — a previous load would
	// otherwise make bootstrap fail with "Service is disabled".
	_ = run("launchctl", "bootout", launchdTarget())
	if err := run("launchctl", "bootstrap", launchdDomain(), path); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	return nil
}

func (l *launchd) uninstall(home string) error {
	_ = run("launchctl", "bootout", launchdTarget())
	return os.Remove(launchdPlistPath(home))
}

func (l *launchd) start() error {
	return run("launchctl", "kickstart", launchdTarget())
}

func (l *launchd) stop() error {
	// kill -SIGTERM via launchctl; KeepAlive would relaunch on real `stop`,
	// so use kill which preserves the "managed but currently dead" state.
	return run("launchctl", "kill", "SIGTERM", launchdTarget())
}

func (l *launchd) restart() error {
	return run("launchctl", "kickstart", "-k", launchdTarget())
}

func (l *launchd) status() (string, error) {
	out, err := exec.Command("launchctl", "list", serviceLabel).CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	// `launchctl list <label>` returns a plist-ish dict with "PID" = N or -.
	pid := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "\"PID\"") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				pid = strings.TrimRight(strings.TrimSpace(parts[1]), ";")
			}
		}
	}
	if pid == "" || pid == "-" {
		return "installed, not running", nil
	}
	if n, err := strconv.Atoi(pid); err == nil {
		return fmt.Sprintf("running (pid %d)", n), nil
	}
	return "running", nil
}

// ---------- systemd --user (Linux) ----------

type systemdUser struct{}

const systemdUnitName = "supervaisor.service"

func systemdUnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
}

func (s *systemdUser) install(home, exe, log string) error {
	unit := fmt.Sprintf(`[Unit]
Description=supervAIsor poller
After=network.target

[Service]
Type=simple
ExecStart=%s run
Restart=always
RestartSec=2
StandardOutput=append:%s
StandardError=append:%s
Environment=HOME=%s

[Install]
WantedBy=default.target
`, exe, log, log, home)

	path := systemdUnitPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	return run("systemctl", "--user", "enable", "--now", systemdUnitName)
}

func (s *systemdUser) uninstall(home string) error {
	_ = run("systemctl", "--user", "disable", "--now", systemdUnitName)
	if err := os.Remove(systemdUnitPath(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = run("systemctl", "--user", "daemon-reload")
	return nil
}

func (s *systemdUser) start() error {
	return run("systemctl", "--user", "start", systemdUnitName)
}

func (s *systemdUser) stop() error {
	return run("systemctl", "--user", "stop", systemdUnitName)
}

func (s *systemdUser) restart() error {
	return run("systemctl", "--user", "restart", systemdUnitName)
}

func (s *systemdUser) status() (string, error) {
	out, _ := exec.Command("systemctl", "--user", "is-active", systemdUnitName).Output()
	state := strings.TrimSpace(string(out))
	if state == "active" {
		return "running", nil
	}
	if state == "inactive" || state == "failed" {
		return state, nil
	}
	return "not installed", nil
}

// run wraps exec.Command with combined-output error reporting so failures
// surface the underlying launchctl/systemctl message instead of a bare exit
// code.
func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return nil
}
