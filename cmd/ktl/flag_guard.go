// File: cmd/ktl/flag_guard.go
// Brief: CLI command wiring and implementation for 'flag guard'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"fmt"
	"strconv"
	"strings"
)

func enforceStrictShortFlags(args []string) error {
	stop := false
	for _, arg := range args {
		if stop {
			break
		}
		if arg == "--" {
			stop = true
			continue
		}
		if strings.HasPrefix(arg, "-A") && arg != "-A" {
			return fmt.Errorf("flag -A must be passed as '-A' without extra characters (received %q)", arg)
		}
		if strings.HasPrefix(arg, "-n") && arg != "-n" {
			return fmt.Errorf("flag -n requires a space-separated namespace value (example: '-n ktl-logger'); received %q", arg)
		}
	}
	return nil
}

type optionalValueRule struct {
	allowNegativeNumeric bool
}

var optionalValueFlags = map[string]optionalValueRule{
	"--ui":        {},
	"--ws-listen": {},
	"--tail":      {allowNegativeNumeric: true},
	"-t":          {allowNegativeNumeric: true},
	"--since":     {},
	"-s":          {},
}

func normalizeOptionalValueArgs(args []string) []string {
	if len(args) <= 1 {
		return args
	}
	normalized := make([]string, 0, len(args))
	normalized = append(normalized, args[0])
	for i := 1; i < len(args); {
		arg := args[i]
		if arg == "--" {
			normalized = append(normalized, args[i:]...)
			break
		}
		rule, ok := optionalValueFlags[arg]
		if !ok || strings.Contains(arg, "=") {
			normalized = append(normalized, arg)
			i++
			continue
		}
		if i+1 >= len(args) {
			normalized = append(normalized, arg)
			i++
			continue
		}
		next := args[i+1]
		if shouldAttachOptionalValue(rule, next) {
			normalized = append(normalized, fmt.Sprintf("%s=%s", arg, next))
			i += 2
			continue
		}
		normalized = append(normalized, arg)
		i++
	}
	if len(normalized) == len(args) {
		return normalized
	}
	return normalized
}

func shouldAttachOptionalValue(rule optionalValueRule, next string) bool {
	if next == "" || next == "--" {
		return false
	}
	if strings.HasPrefix(next, "-") {
		if !rule.allowNegativeNumeric {
			return false
		}
		if len(next) == 1 {
			return false
		}
		if _, err := strconv.Atoi(next); err != nil {
			return false
		}
		return true
	}
	return true
}
