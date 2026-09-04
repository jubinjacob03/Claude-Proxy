//go:build windows

package main

import (
	"strings"

	"fyne.io/systray"
)

// Menu labels stay provider-neutral: the split is an internal cost measure and
// nothing about it should be obvious to whoever is using the proxy.
//
// The licence key is the only credential this app ever holds. Upstream API keys
// live on the relay, so there is deliberately no menu item for one.
type settingsMenu struct {
	envPath string

	licenseKey *systray.MenuItem
}

func newSettingsMenu(parent *systray.MenuItem, envPath string) *settingsMenu {
	m := &settingsMenu{envPath: envPath}

	m.licenseKey = parent.AddSubMenuItem("Licence key", "Enter a new licence key and restart the proxy")

	return m
}

func (m *settingsMenu) watch() {
	go func() {
		for range m.licenseKey.ClickedCh {
			m.editSecret("LICENSE_KEY", "Licence key",
				"Paste your licence key. The proxy restarts and activates this machine.")
		}
	}()
}

func (m *settingsMenu) editValue(key, title, prompt string) {
	value, ok := promptValue(title, prompt, getEnvValue(m.envPath, key))
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		notify("Claude proxy", "Nothing entered, so nothing changed.")
		return
	}
	m.apply(key, value, title+" updated.")
}

// editSecret never prefills, so a secret is not shown back to whoever opens the box.
func (m *settingsMenu) editSecret(key, title, prompt string) {
	value, ok := promptValue(title, prompt, "")
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		notify("Claude proxy", "Nothing entered, so nothing changed.")
		return
	}
	m.apply(key, value, title+" updated.")
}
func (m *settingsMenu) apply(key, value, message string) {
	if err := setEnvValue(m.envPath, key, value); err != nil {
		notify("Claude proxy", "failed to update .env: "+err.Error())
		return
	}
	notify("Claude proxy", message+" Restarting proxy...")
	sup.triggerRestart()
}
