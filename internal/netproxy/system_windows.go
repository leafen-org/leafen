//go:build windows

package netproxy

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// platformSettings reads Windows' proxy settings (Settings › Network &
// internet › Proxy), the person's Internet Settings.
func platformSettings() systemSettings {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return systemSettings{}
	}
	defer key.Close()
	enabled, _, _ := key.GetIntegerValue("ProxyEnable")
	server, _, _ := key.GetStringValue("ProxyServer")
	override, _, _ := key.GetStringValue("ProxyOverride")
	autoConfig, _, _ := key.GetStringValue("AutoConfigURL")
	return readInternetSettings(enabled == 1, server, override, autoConfig, autoDetectOn())
}

// readInternetSettings reads ProxyServer — one "host:port" for all, or
// "http=host:port;https=host:port;socks=…" — and ProxyOverride, the
// addresses reached directly separated by ";", "<local>" naming names
// without a dot; and the automatic configuration: a script's address, and
// whether the network gives one (WPAD).
func readInternetSettings(enabled bool, server, override, autoConfig string, autoDetect bool) systemSettings {
	settings := systemSettings{autoConfig: strings.TrimSpace(autoConfig), autoDiscover: autoDetect}
	if enabled && strings.TrimSpace(server) != "" {
		if !strings.Contains(server, "=") {
			proxy := "http://" + strings.TrimSpace(server)
			settings.config.HTTPProxy, settings.config.HTTPSProxy = proxy, proxy
		} else {
			for _, part := range strings.Split(server, ";") {
				scheme, address, _ := strings.Cut(strings.TrimSpace(part), "=")
				switch strings.ToLower(scheme) {
				case "http":
					settings.config.HTTPProxy = "http://" + address
				case "https":
					settings.config.HTTPSProxy = "http://" + address
				}
			}
		}
	}
	exceptions := []string{}
	for _, entry := range strings.Split(override, ";") {
		entry = strings.TrimSpace(entry)
		if strings.EqualFold(entry, "<local>") {
			settings.simpleDirect = true
		} else if entry != "" {
			exceptions = append(exceptions, entry)
		}
	}
	settings.config.NoProxy = noProxyOf(exceptions)
	if settings.config.HTTPProxy == "" && settings.config.HTTPSProxy == "" && enabled && strings.Contains(strings.ToLower(server), "socks=") {
		settings.unfollowed = "socks"
	}
	return settings
}
