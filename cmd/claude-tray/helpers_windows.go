//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow         = 0x08000000
	detachedProcess        = 0x00000008
	createBreakawayFromJob = 0x01000000
)

var jobHandle windows.Handle

func initJobObject() {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(h)
		return
	}
	jobHandle = h
}

func assignToJob(pid int) {
	if jobHandle == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(jobHandle, h)
}

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	mbOK              = 0x00000000
	mbIconInformation = 0x00000040
	mbSetForeground   = 0x00010000
	mbTopMost         = 0x00040000
)

func notify(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(msgPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbOK|mbIconInformation|mbSetForeground|mbTopMost),
	)
}

// promptValue shows a Windows input box and returns the entered text. ok is
// false when the box could not be shown or the user cancelled.
func promptValue(title, message, current string) (string, bool) {
	script := `Add-Type -AssemblyName Microsoft.VisualBasic; ` +
		`[Microsoft.VisualBasic.Interaction]::InputBox(` +
		psQuote(message) + `,` + psQuote(title) + `,` + psQuote(current) + `)`

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	value := strings.TrimRight(string(out), "\r\n")
	if value == "" {
		return "", false
	}
	return value, true
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func openMonitor(logPath string) {
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	if _, err := os.Stat(logPath); err != nil {
		if f, cerr := os.Create(logPath); cerr == nil {
			f.Close()
		}
	}

	scriptPath := filepath.Join(filepath.Dir(logPath), monitorScriptName)
	if err := os.WriteFile(scriptPath, []byte(monitorScript(logPath)), 0o644); err != nil {
		notify("Claude proxy", "Could not write the Monitor script:\n"+err.Error())
		return
	}

	cmd := exec.Command("cmd", "/c", "start", "Claude Proxy - Monitor",
		"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-NoExit", "-File", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	_ = cmd.Start()
}

func monitorScript(logPath string) string {
	quoted := "'" + strings.ReplaceAll(logPath, "'", "''") + "'"

	return strings.Join([]string{
		`$ErrorActionPreference = 'Continue'`,
		`try { $Host.UI.RawUI.WindowTitle = 'Claude Proxy - Monitor' } catch {}`,
		`try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}`,
		`$path = ` + quoted,
		`Write-Host ('Following ' + $path) -ForegroundColor DarkGray`,
		`Write-Host 'Closing this window stops watching only; the proxy keeps running.' -ForegroundColor DarkGray`,
		`Write-Host ''`,
		`$backlogMax = 262144`,
		`$pos = 0`,
		`try {`,
		`  $len = (Get-Item -LiteralPath $path).Length`,
		`  if ($len -gt $backlogMax) { $pos = $len - $backlogMax }`,
		`} catch {}`,
		`while ($true) {`,
		`  try {`,
		`    $share = [System.IO.FileShare]::ReadWrite -bor [System.IO.FileShare]::Delete`,
		`    $fs = New-Object System.IO.FileStream($path, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, $share)`,
		`    if ($fs.Length -lt $pos) { $pos = 0 }`,
		`    [void]$fs.Seek($pos, [System.IO.SeekOrigin]::Begin)`,
		`    $sr = New-Object System.IO.StreamReader($fs, [System.Text.Encoding]::UTF8)`,
		`    $chunk = $sr.ReadToEnd()`,
		`    $pos = $fs.Position`,
		`    $sr.Dispose()`,
		`    $fs.Dispose()`,
		`    if ($chunk.Length -gt 0) { Write-Host -NoNewline -Object $chunk }`,
		`  } catch {`,
		`    Start-Sleep -Milliseconds 700`,
		`  }`,
		`  Start-Sleep -Milliseconds 300`,
		`}`,
	}, "\n")
}

func setEnvValue(path, key, value string) error {
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		lines = strings.Split(string(data), "\n")
	}

	newLine := key + "=" + value
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "export ")
		if eq := strings.IndexByte(trimmed, '='); eq > 0 {
			if strings.TrimSpace(trimmed[:eq]) == key {
				lines[i] = newLine
				replaced = true
				break
			}
		}
	}
	if !replaced {
		lines = append(lines, newLine)
	}

	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

func getEnvValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 || strings.TrimSpace(trimmed[:eq]) != key {
			continue
		}
		val := strings.TrimSpace(trimmed[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		return val
	}
	return ""
}

func launchInstaller(installerPath string, relaunch bool) error {
	args := []string{"/S"}
	if relaunch {
		args = append(args, "/RESTART")
	}
	cmd := exec.Command(installerPath, args...)
	cmd.Dir = filepath.Dir(installerPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | detachedProcess | createBreakawayFromJob,
	}
	if err := cmd.Start(); err != nil {
		cmd = exec.Command(installerPath, args...)
		cmd.Dir = filepath.Dir(installerPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: createNoWindow | detachedProcess,
		}
		if err2 := cmd.Start(); err2 != nil {
			return err2
		}
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func logUpdate(format string, args ...interface{}) {
	line := time.Now().Format("2006/01/02 15:04:05") + " " + fmt.Sprintf(format, args...) + "\r\n"
	if sup == nil {
		return
	}
	path := filepath.Join(sup.dir, "logs", "update.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	instanceHandle   uintptr
)

const errAlreadyExists = syscall.Errno(183)

func acquireSingleInstance(name string) bool {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return true
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		h, _, lastErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
		if h == 0 {
			return true
		}
		if lastErr != errAlreadyExists {
			instanceHandle = h
			return true
		}
		windows.CloseHandle(windows.Handle(h))
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(400 * time.Millisecond)
	}
}
