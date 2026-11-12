package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// shortIntervalWarning is the interval below which StartupWarnings reports a warning.
const shortIntervalWarning = time.Minute

const environmentPrefix = "HARBORBUDDY_"

// Warning is a non-fatal configuration problem to report at startup.
type Warning struct {
	Setting string
	Message string
}

// otherEnvironment lists HARBORBUDDY_* variables read outside ApplyEnvironment: the config
// path (internal/app), the health status file (internal/health), and self-update identity
// (internal/selfupdate).
var otherEnvironment = []string{
	"HARBORBUDDY_CONFIG",
	"HARBORBUDDY_HEALTH_FILE",
	"HARBORBUDDY_CONTAINER_NAME",
	"HARBORBUDDY_CONTAINER_ID",
}

// environmentSuggestions maps likely mistakes to the variable HarborBuddy reads.
var environmentSuggestions = map[string]string{
	"HARBORBUDDY_SELF_UPDATE":            "HARBORBUDDY_SELF_UPDATE_ENABLED",
	"HARBORBUDDY_UPDATES_SELF_UPDATE":    "HARBORBUDDY_SELF_UPDATE_ENABLED",
	"HARBORBUDDY_CHECK_INTERVAL":         "HARBORBUDDY_INTERVAL",
	"HARBORBUDDY_UPDATES_CHECK_INTERVAL": "HARBORBUDDY_INTERVAL",
	"HARBORBUDDY_UPDATES_SCHEDULE_TIME":  "HARBORBUDDY_SCHEDULE_TIME",
	"HARBORBUDDY_SCHEDULE":               "HARBORBUDDY_SCHEDULE_TIME",
	"HARBORBUDDY_UPDATES_TIMEZONE":       "HARBORBUDDY_TIMEZONE",
	"HARBORBUDDY_TZ":                     "HARBORBUDDY_TIMEZONE",
	"HARBORBUDDY_UPDATES_DRY_RUN":        "HARBORBUDDY_DRY_RUN",
	"HARBORBUDDY_DRYRUN":                 "HARBORBUDDY_DRY_RUN",
	"HARBORBUDDY_UPDATES":                "HARBORBUDDY_UPDATES_ENABLED",
	"HARBORBUDDY_ENABLED":                "HARBORBUDDY_UPDATES_ENABLED",
	"HARBORBUDDY_CLEANUP":                "HARBORBUDDY_CLEANUP_ENABLED",
	"HARBORBUDDY_DOCKER":                 "HARBORBUDDY_DOCKER_HOST",
	"HARBORBUDDY_LOG_MAX_BACKUP":         "HARBORBUDDY_LOG_MAX_BACKUPS",
	"HARBORBUDDY_JSON":                   "HARBORBUDDY_LOG_JSON",
	"HARBORBUDDY_LEVEL":                  "HARBORBUDDY_LOG_LEVEL",
}

// StartupWarnings reports settings that are valid but probably not what the operator meant.
// environ holds KEY=VALUE pairs, as from os.Environ.
func StartupWarnings(cfg Config, environ []string) []Warning {
	var warnings []Warning
	if cfg.Updates.ScheduleTime == "" && !cfg.RunOnce && !cfg.CleanupOnly && cfg.Updates.CheckInterval < shortIntervalWarning {
		warnings = append(warnings, Warning{
			Setting: "updates.check_interval",
			Message: fmt.Sprintf("check interval %s is below %s; every cycle pulls each eligible image and may hit registry rate limits",
				cfg.Updates.CheckInterval, shortIntervalWarning),
		})
	}
	if cfg.IntervalOverride && cfg.Updates.ScheduleTime != "" && !cfg.RunOnce && !cfg.CleanupOnly {
		warnings = append(warnings, Warning{
			Setting: "updates.check_interval",
			Message: fmt.Sprintf("the check interval set by HARBORBUDDY_INTERVAL or --interval is ignored because schedule_time %s is set; the daily schedule takes precedence",
				cfg.Updates.ScheduleTime),
		})
	}
	if warning, ok := unusedTimezoneWarning(cfg, environ); ok {
		warnings = append(warnings, warning)
	}
	return append(warnings, unknownEnvironment(environ)...)
}

// unusedTimezoneWarning reports a timezone that does not load when no daily
// schedule uses it, where validation does not check it. A value taken from TZ
// is left alone: TZ serves other programs and accepts forms Go does not.
func unusedTimezoneWarning(cfg Config, environ []string) (Warning, bool) {
	zone := cfg.Updates.Timezone
	if cfg.Updates.ScheduleTime != "" || zone == "" || slices.Contains(environ, "TZ="+zone) {
		return Warning{}, false
	}
	if _, err := time.LoadLocation(zone); err == nil {
		return Warning{}, false
	}
	return Warning{
		Setting: "updates.timezone",
		Message: fmt.Sprintf("timezone %q is not a valid IANA zone; it is unused until schedule_time is set, and will then stop startup", zone),
	}, true
}

func unknownEnvironment(environ []string) []Warning {
	known := knownEnvironment()
	var names []string
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, environmentPrefix) && !known[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	warnings := make([]Warning, 0, len(names))
	for _, name := range names {
		message := "unrecognized environment variable is ignored"
		if suggestion, ok := environmentSuggestions[name]; ok {
			message += "; did you mean " + suggestion + "?"
		}
		warnings = append(warnings, Warning{Setting: name, Message: message})
	}
	return warnings
}

// knownEnvironment records every name ApplyEnvironment reads, so the list cannot drift from
// the parser, plus the variables read elsewhere.
func knownEnvironment() map[string]bool {
	known := make(map[string]bool)
	for _, name := range otherEnvironment {
		known[name] = true
	}
	cfg := Default()
	_ = cfg.ApplyEnvironment(func(name string) string {
		known[name] = true
		return ""
	})
	return known
}
