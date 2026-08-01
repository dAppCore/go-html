package container

import (
	core "dappco.re/go"
	"dappco.re/go/config"
)

type AppMode string

const (
	ModeManager AppMode = "manager"
	ModeWorker  AppMode = "worker"
)

type ModeEnvironment struct {
	Args        []string
	LookupEnv   func(string) (string, bool)
	ConfigValue func(string) string
}

var argsFunc = core.Args

// DetectMode resolves the RFC startup mode from CLI flags first, then config/env.
func DetectMode() AppMode {
	cfg, _ := core.Cast[*config.Config](config.New())
	return DetectModeWithEnvironment(ModeEnvironment{
		Args:      argsFunc()[1:],
		LookupEnv: core.LookupEnv,
		ConfigValue: func(key string) string {
			if cfg == nil {
				return ""
			}
			var value string
			if !cfg.Get(key, &value).OK {
				return ""
			}
			return value
		},
	})
}

func DetectModeWithEnvironment(environment ModeEnvironment) AppMode {
	args := environment.Args
	if args == nil {
		args = argsFunc()[1:]
	}

	if value, found := modeArgValue(args); found {
		if mode, ok := parseMode(value); ok {
			return mode
		}
	}

	if environment.LookupEnv != nil {
		if value, ok := environment.LookupEnv("CORE_GUI_MODE"); ok {
			if mode, ok := parseMode(value); ok {
				return mode
			}
		}
	}

	if environment.ConfigValue != nil {
		for _, key := range []string{"gui.mode", "display.mode", "mode"} {
			if mode, ok := parseMode(environment.ConfigValue(key)); ok {
				return mode
			}
		}
	}

	return ModeManager
}

func modeArgValue(args []string) (string, bool) {
	for i := range args {
		arg := core.Trim(args[i])
		if arg == "--mode" || arg == "-mode" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if value, ok := cutPrefix(arg, "--mode="); ok {
			return value, true
		}
		if value, ok := cutPrefix(arg, "-mode="); ok {
			return value, true
		}
	}
	return "", false
}

func parseMode(value string) (AppMode, bool) {
	switch core.Lower(core.Trim(value)) {
	case string(ModeWorker):
		return ModeWorker, true
	case string(ModeManager):
		return ModeManager, true
	default:
		return "", false
	}
}
