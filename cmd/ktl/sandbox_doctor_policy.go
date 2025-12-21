package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type sandboxPolicySummary struct {
	NetworkMode    string
	RlimitAS       string
	RlimitCPU      string
	RlimitFsize    string
	RlimitNofile   string
	RlimitNproc    string
	TmpfsMounts    []string
	BindMountCount int
}

type sandboxMount struct {
	src    string
	dst    string
	fstype string
	isBind bool
	opts   string
}

func parseSandboxPolicy(path string) (sandboxPolicySummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return sandboxPolicySummary{}, err
	}
	defer f.Close()

	var out sandboxPolicySummary
	out.NetworkMode = "unknown"

	var mounts []sandboxMount
	var current *sandboxMount
	inMount := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "mount {" {
			inMount = true
			current = &sandboxMount{}
			continue
		}
		if inMount && line == "}" {
			inMount = false
			if current != nil {
				mounts = append(mounts, *current)
			}
			current = nil
			continue
		}

		key, val, ok := parseKeyValue(line)
		if !ok {
			continue
		}
		if inMount && current != nil {
			switch key {
			case "src":
				current.src = trimQuotes(val)
			case "dst":
				current.dst = trimQuotes(val)
			case "fstype":
				current.fstype = trimQuotes(val)
			case "is_bind":
				current.isBind = val == "true"
			case "options":
				current.opts = trimQuotes(val)
			}
			continue
		}

		switch key {
		case "clone_newnet":
			if val == "true" {
				out.NetworkMode = "isolated"
			} else if val == "false" {
				out.NetworkMode = "host"
			}
		case "rlimit_as":
			out.RlimitAS = val
		case "rlimit_cpu":
			out.RlimitCPU = val
		case "rlimit_fsize":
			out.RlimitFsize = val
		case "rlimit_nofile":
			out.RlimitNofile = val
		case "rlimit_nproc":
			out.RlimitNproc = val
		}
	}
	if err := scanner.Err(); err != nil {
		return sandboxPolicySummary{}, err
	}

	for _, m := range mounts {
		if m.isBind {
			out.BindMountCount++
		}
		if strings.EqualFold(m.fstype, "tmpfs") && strings.TrimSpace(m.dst) != "" {
			label := m.dst
			if strings.TrimSpace(m.opts) != "" {
				label = fmt.Sprintf("%s (%s)", m.dst, m.opts)
			}
			out.TmpfsMounts = append(out.TmpfsMounts, label)
		}
	}

	return out, nil
}

func parseKeyValue(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	if key == "" || val == "" {
		return "", "", false
	}
	return key, val, true
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, `"`)
	s = strings.TrimSuffix(s, `"`)
	return s
}
