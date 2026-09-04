//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"
)

//go:embed icon.ico
var trayIcon []byte

const (
	proxyExeName = "claude-proxy.exe"
	envFileName  = ".env"
	consoleLog   = "proxy-console.log"

	consoleLogSeparator = "--------------------------------------------------------------"
	consoleLogMaxBytes  = 8 << 20

	fastExitThreshold     = 3 * time.Second
	fastExitsBeforeNotify = 3

	monitorScriptName = "monitor.ps1"

	instanceMutexName = "Claude-Proxy-Tray-Singleton"
)

type supervisor struct {
	dir         string
	exePath     string
	consolePath string

	mu       sync.Mutex
	cmd      *exec.Cmd
	exited   chan struct{}
	stopping atomic.Bool

	restart  chan struct{}
	quitOnce sync.Once
	quit     chan struct{}
}

func newSupervisor() (*supervisor, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(exe)
	s := &supervisor{
		dir:         dir,
		exePath:     filepath.Join(dir, proxyExeName),
		consolePath: filepath.Join(dir, "logs", consoleLog),
		restart:     make(chan struct{}, 1),
		quit:        make(chan struct{}),
	}
	if _, err := os.Stat(s.exePath); err != nil {
		return nil, fmt.Errorf("%s not found next to the tray app: %w", proxyExeName, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		notify("Claude proxy", "could not create logs directory: "+err.Error())
	}
	return s, nil
}

func (s *supervisor) run() {
	backoff := time.Second
	fastExits := 0
	for {
		startedAt := time.Now()
		exited := s.start()
		if exited == nil {
			select {
			case <-s.quit:
				return
			case <-time.After(backoff):
			}
			backoff = capDur(backoff*2, 30*time.Second)
			continue
		}

		select {
		case <-s.quit:
			s.killChild()
			return
		case <-s.restart:
			s.killChild()
			<-exited
		case <-exited:
			if s.stopping.Load() {
				select {
				case <-s.quit:
					return
				case <-s.restart:
				}
				continue
			}
			if time.Since(startedAt) < fastExitThreshold {
				fastExits++
				if fastExits == fastExitsBeforeNotify {
					logUpdate("proxy exited immediately %d times; surfacing to the user", fastExits)
					notify("Claude proxy", "The proxy keeps exiting as soon as it starts. Use Monitor Logs to inspect the failure.")
				}
			} else {
				fastExits = 0
				backoff = time.Second
			}
			select {
			case <-s.quit:
				return
			case <-time.After(backoff):
			}
			if fastExits > 0 {
				backoff = capDur(backoff*2, 30*time.Second)
			}
		}
	}
}

func (s *supervisor) start() <-chan struct{} {
	s.stopping.Store(false)

	logFile := s.openConsoleLog()

	cmd := exec.Command(s.exePath)
	cmd.Dir = s.dir
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	if err := cmd.Start(); err != nil {
		notify("Claude proxy", "failed to start proxy: "+err.Error())
		if logFile != nil {
			logFile.Close()
		}
		return nil
	}

	assignToJob(cmd.Process.Pid)

	exited := make(chan struct{})
	s.mu.Lock()
	s.cmd = cmd
	s.exited = exited
	s.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		if logFile != nil {
			fmt.Fprintf(logFile, "\n===== proxy stopped %s =====\n%s\n",
				time.Now().Format("2006-01-02 15:04:05"), consoleLogSeparator)
			logFile.Close()
		}
		close(exited)
	}()
	return exited
}

func (s *supervisor) openConsoleLog() *os.File {
	if info, err := os.Stat(s.consolePath); err == nil && info.Size() > consoleLogMaxBytes {
		previous := s.consolePath + ".1"
		_ = os.Remove(previous)
		_ = os.Rename(s.consolePath, previous)
	}

	f, err := os.OpenFile(s.consolePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logUpdate("cannot open %s (%v); proxy output will not be captured", s.consolePath, err)
		notify("Claude proxy", "Cannot open the console log:\n"+err.Error())
		return nil
	}
	fmt.Fprintf(f, "\n===== proxy started %s =====\n", time.Now().Format("2006-01-02 15:04:05"))
	return f
}

func (s *supervisor) killChild() {
	s.mu.Lock()
	cmd, exited := s.cmd, s.exited
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	s.stopping.Store(true)
	_ = cmd.Process.Kill()
	if exited != nil {
		select {
		case <-exited:
		case <-time.After(6 * time.Second):
		}
	}
}

func (s *supervisor) triggerRestart() {
	s.stopping.Store(true)
	select {
	case s.restart <- struct{}{}:
	default:
	}
	s.killChild()
}

func (s *supervisor) shutdown() {
	s.stopping.Store(true)
	s.quitOnce.Do(func() { close(s.quit) })
	s.killChild()
}

func capDur(d, max time.Duration) time.Duration {
	if d > max {
		return max
	}
	return d
}

var (
	sup           *supervisor
	spl           *splash
	upd           *updater
	updCancel     context.CancelFunc
	mUpdate       *systray.MenuItem
	updateApplied atomic.Bool
)

func main() {
	if !acquireSingleInstance(instanceMutexName) {
		return
	}

	s, err := newSupervisor()
	if err != nil {
		notify("Claude proxy", err.Error())
		os.Exit(1)
	}
	sup = s

	spl = newSplash(sup.dir)
	if applyUpdateBeforeStart(spl) {
		return
	}

	spl.set(splashIndeterminate, "Starting Claude Proxy...")
	initJobObject()
	go s.run()
	go func() {
		waitProxyReady(sup.dir, 20*time.Second)
		spl.close()
	}()
	systray.Run(onReady, onExit)
}

func applyUpdateBeforeStart(sp *splash) bool {
	if !updatable() || !autoUpdateEnabled(filepath.Join(sup.dir, envFileName)) {
		return false
	}
	u := newUpdater(sup.dir, nil)
	u.onStatus = sp.set
	return u.applyStartupUpdate(context.Background())
}

func onReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("Claude Proxy")
	systray.SetTooltip("Claude Proxy " + appVersion + " - running")

	mMonitor := systray.AddMenuItem("Monitor Logs", "Open a window with the live proxy logs")
	mSettings := systray.AddMenuItem("Settings", "Configure the proxy")
	newSettingsMenu(mSettings, filepath.Join(sup.dir, envFileName)).watch()
	mUpdate = systray.AddMenuItem("Restart to Update", "Install the downloaded update and restart")
	mUpdate.Hide()
	systray.AddSeparator()
	mExit := systray.AddMenuItem("Exit proxy", "Stop this proxy instance (restarts on next login)")

	startUpdater()

	go func() {
		for {
			select {
			case <-mMonitor.ClickedCh:
				openMonitor(sup.consolePath)
			case <-mUpdate.ClickedCh:
				if handleRestartToUpdate() {
					return
				}
			case <-mExit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func startUpdater() {
	if !updatable() {
		return
	}
	if !autoUpdateEnabled(filepath.Join(sup.dir, envFileName)) {
		return
	}
	upd = newUpdater(sup.dir, onUpdateStaged)
	ctx, cancel := context.WithCancel(context.Background())
	updCancel = cancel
	go upd.run(ctx)
}

func onUpdateStaged(version string) {
	if mUpdate == nil {
		return
	}
	mUpdate.SetTitle("Restart to Update (v" + version + ")")
	mUpdate.SetTooltip("Version " + version + " is downloaded and verified. Install it now and restart.")
	mUpdate.Show()
	systray.SetTooltip("Claude Proxy " + appVersion + " - update to v" + version + " ready")
}

func handleRestartToUpdate() bool {
	version, _, ok := upd.staged()
	if !ok {
		notify("Claude proxy", "No update is staged yet.")
		return false
	}
	updateApplied.Store(true)
	if err := upd.applyNow(true); err != nil {
		updateApplied.Store(false)
		logUpdate("apply failed: %v", err)
		notify("Claude proxy", "Update to v"+version+" failed to start:\n"+err.Error()+"\n\nThe proxy is still running.")
		return false
	}
	if updCancel != nil {
		updCancel()
	}
	if sup != nil {
		sup.shutdown()
	}
	systray.Quit()
	return true
}

func onExit() {
	if updCancel != nil {
		updCancel()
	}
	if sup != nil {
		sup.shutdown()
	}
	if upd == nil || updateApplied.Load() {
		return
	}
	if _, _, ok := upd.staged(); !ok {
		return
	}
	if err := upd.applyNow(false); err != nil {
		logUpdate("apply on exit failed: %v", err)
	}
}
