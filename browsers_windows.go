//go:build windows

package main

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func init() { detectBrowsers = detectWindowsBrowsers }

// Browsers register themselves under Clients\StartMenuInternet (per machine or per user).
func detectWindowsBrowsers() []browserInfo {
	var out []browserInfo
	seen := map[string]bool{}
	roots := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Clients\StartMenuInternet`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Clients\StartMenuInternet`},
		{registry.CURRENT_USER, `SOFTWARE\Clients\StartMenuInternet`},
	}
	for _, r := range roots {
		k, err := registry.OpenKey(r.root, r.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, sub := range subs {
			name := readRegDefault(r.root, r.path+`\`+sub)
			cmd := readRegDefault(r.root, r.path+`\`+sub+`\shell\open\command`)
			exe := windowsExe(cmd)
			if name == "" || exe == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, browserInfo{ID: "reg:" + sub, Name: name, cmd: []string{exe, urlToken}})
		}
	}
	return out
}

func readRegDefault(root registry.Key, path string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	if err != nil {
		return ""
	}
	return v
}

// windowsExe extracts the program path from a registry command such as `"C:\...\chrome.exe" --flag`.
func windowsExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.Index(cmd[1:], `"`); i >= 0 {
			return cmd[1 : i+1]
		}
		return ""
	}
	if i := strings.Index(strings.ToLower(cmd), ".exe"); i >= 0 {
		return cmd[:i+4]
	}
	return cmd
}
