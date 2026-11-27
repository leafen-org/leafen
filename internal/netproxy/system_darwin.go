//go:build darwin && !ios

package netproxy

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// platformSettings reads macOS's proxy settings (System Settings › Network
// › Details › Proxies) as scutil prints them.
func platformSettings() systemSettings {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return systemSettings{}
	}
	return readScutil(string(output))
}

// readScutil reads scutil's dictionary: "Key : value" lines, and the
// exceptions as an array of "N : value" lines.
func readScutil(text string) systemSettings {
	values := map[string]string{}
	exceptions := []string{}
	inExceptions := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if inExceptions {
			if line == "}" {
				inExceptions = false
				continue
			}
			if _, value, ok := strings.Cut(line, " : "); ok {
				exceptions = append(exceptions, strings.TrimSpace(value))
			}
			continue
		}
		key, value, ok := strings.Cut(line, " : ")
		if !ok {
			continue
		}
		if key == "ExceptionsList" {
			inExceptions = true
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	settings := systemSettings{simpleDirect: values["ExcludeSimpleHostnames"] == "1"}
	if values["HTTPEnable"] == "1" && values["HTTPProxy"] != "" {
		settings.config.HTTPProxy = "http://" + joinPort(values["HTTPProxy"], values["HTTPPort"], "80")
	}
	if values["HTTPSEnable"] == "1" && values["HTTPSProxy"] != "" {
		settings.config.HTTPSProxy = "http://" + joinPort(values["HTTPSProxy"], values["HTTPSPort"], "443")
	}
	settings.config.NoProxy = noProxyOf(exceptions)
	if values["ProxyAutoConfigEnable"] == "1" {
		settings.autoConfig = values["ProxyAutoConfigURLString"]
	}
	settings.autoDiscover = values["ProxyAutoDiscoveryEnable"] == "1"
	if settings.config.HTTPProxy == "" && settings.config.HTTPSProxy == "" && values["SOCKSEnable"] == "1" {
		settings.unfollowed = "socks"
	}
	return settings
}
