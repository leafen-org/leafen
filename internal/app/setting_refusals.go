package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The settings the server was given and cannot use (2026-11-03: a memory
// limit of 100m, abc or 0 started the server on half of the machine's memory
// and nothing said so). Each still falls back as it always did; this is the
// one place that says so, at the start and in the doctor command. Values are
// not repeated, since a public address can carry a password.

var settingChecks = []struct {
	name  string
	check func(raw string) error
	using func() string
}{
	{memoryLimitEnvironment, func(raw string) error {
		_, err := parseMemoryOverrideMiB(raw)
		return err
	}, func() string {
		limit, source := serverMemoryLimit()
		return fmt.Sprintf("%d MiB from %s", limit/memoryMiB, source)
	}},
	{"LEAFEN_PUBLIC_URL", func(string) error {
		if configuredPublicAppHostname() == "" {
			return errors.New("it must be an http or https address with nothing after the host, such as https://leafen.example.com")
		}
		return nil
	}, fixedSetting("the address each request comes in on")},
	{scheduledBackupIntervalEnv, func(raw string) error {
		if _, ok := parseScheduledBackupInterval(raw); !ok {
			return fmt.Errorf("it must be hours (12, 12h), days (2d), hourly, daily or weekly, at most %d hours", maxScheduledBackupIntervalHours)
		}
		return nil
	}, fixedSetting("daily")},
	{scheduledBackupKeepEnv, func(raw string) error {
		if _, ok := parseScheduledBackupKeep(raw); !ok {
			return fmt.Errorf("it must be a number from 1 to %d", maxScheduledBackupKeep)
		}
		return nil
	}, fixedSetting(strconv.Itoa(scheduledBackupDefaultKeep))},
	{scheduledBackupS3PathStyleEnv, func(raw string) error {
		if _, err := strconv.ParseBool(raw); err != nil {
			return errors.New("it must be true or false")
		}
		return nil
	}, fixedSetting("false")},
	{replicationEnv, func(string) error {
		if set, _ := replicationFromEnvironment(); !set {
			return errors.New("it must be on or off")
		}
		return nil
	}, fixedSetting("the setting in Standby server")},
	{tunnelDoorPortEnv, func(raw string) error {
		if port, err := strconv.Atoi(raw); err != nil || port < 1 || port > 65535 {
			return errors.New("it must be a port from 1 to 65535")
		}
		return nil
	}, fixedSetting("the server's port plus one")},
	{setupOpenModeEnvironment, oneOfSetting(setupOpenModePrint, setupOpenModeBackground), fixedSetting(setupOpenModePrint)},
	{setupWithoutCodeEnv, oneOfSetting(switchSettingValues...), fixedSetting("the setup code")},
	{"LEAFEN_PLACE_SEARCH_DISABLED", oneOfSetting(switchSettingValues...), fixedSetting("place search as set on the server's settings")},
	{updateCheckEnv, oneOfSetting("off", "on"), fixedSetting("on")},
	{sponsorCreditEnv, oneOfSetting("off", "on"), fixedSetting("on")},
	{requestLimitProfileEnvironment, oneOfSetting("production", "development", "test"), fixedSetting("production")},
}

var switchSettingValues = []string{"1", "true", "on", "yes", "0", "false", "off", "no"}

// settingRefusals says, for each setting given that cannot be used, why and
// what the server uses instead.
func settingRefusals() []string {
	refusals := []string{}
	for _, setting := range settingChecks {
		raw := strings.TrimSpace(os.Getenv(setting.name))
		if raw == "" {
			continue
		}
		if err := setting.check(raw); err != nil {
			refusals = append(refusals, fmt.Sprintf("%s is not used: %s; using %s", setting.name, strings.TrimPrefix(err.Error(), setting.name+" "), setting.using()))
		}
	}
	return refusals
}

func fixedSetting(value string) func() string {
	return func() string { return value }
}

func oneOfSetting(values ...string) func(string) error {
	return func(raw string) error {
		for _, value := range values {
			if strings.EqualFold(raw, value) {
				return nil
			}
		}
		return fmt.Errorf("it must be %s", strings.Join(values, " or "))
	}
}
