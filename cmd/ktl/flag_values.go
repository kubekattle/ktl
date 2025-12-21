// File: cmd/ktl/flag_values.go
// Brief: pflag.Value implementations for parse-time validation.

package main

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
)

type enumStringValue struct {
	dest    *string
	allowed map[string]struct{}
}

func newEnumStringValue(dest *string, allowed ...string) *enumStringValue {
	m := make(map[string]struct{}, len(allowed))
	for _, v := range allowed {
		m[v] = struct{}{}
	}
	return &enumStringValue{dest: dest, allowed: m}
}

func (v *enumStringValue) String() string {
	if v == nil || v.dest == nil {
		return ""
	}
	return *v.dest
}

func (v *enumStringValue) Set(s string) error {
	s = strings.TrimSpace(s)
	if _, ok := v.allowed[s]; !ok {
		return fmt.Errorf("must be one of: %s", strings.Join(v.allowedValues(), ", "))
	}
	*v.dest = s
	return nil
}

func (v *enumStringValue) Type() string { return "string" }

func (v *enumStringValue) allowedValues() []string {
	values := make([]string, 0, len(v.allowed))
	for k := range v.allowed {
		values = append(values, k)
	}
	sort.Strings(values)
	return values
}

type optionalBoolStringValue struct {
	dest *string
}

func (v *optionalBoolStringValue) String() string {
	if v == nil || v.dest == nil {
		return ""
	}
	return *v.dest
}

func (v *optionalBoolStringValue) Set(s string) error {
	raw := strings.TrimSpace(s)
	if raw == "" {
		*v.dest = ""
		return nil
	}
	normalized := strings.ToLower(raw)
	if normalized != "true" && normalized != "false" {
		return fmt.Errorf("must be true or false")
	}
	*v.dest = normalized
	return nil
}

func (v *optionalBoolStringValue) Type() string { return "string" }

type nonNegativeIntValue struct {
	dest *int
}

func (v *nonNegativeIntValue) String() string {
	if v == nil || v.dest == nil {
		return "0"
	}
	return strconv.Itoa(*v.dest)
}

func (v *nonNegativeIntValue) Set(s string) error {
	raw := strings.TrimSpace(s)
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("must be an integer")
	}
	if n < 0 {
		return fmt.Errorf("must be >= 0")
	}
	*v.dest = n
	return nil
}

func (v *nonNegativeIntValue) Type() string { return "int" }

type validatedStringArrayValue struct {
	dest      *[]string
	validator func(string) error
	name      string
}

func (v *validatedStringArrayValue) String() string {
	if v == nil || v.dest == nil {
		return ""
	}
	return strings.Join(*v.dest, ",")
}

func (v *validatedStringArrayValue) Set(s string) error {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return fmt.Errorf("%s cannot be empty", v.name)
	}
	if v.validator != nil {
		if err := v.validator(raw); err != nil {
			return err
		}
	}
	*v.dest = append(*v.dest, raw)
	return nil
}

func (v *validatedStringArrayValue) Type() string { return "stringArray" }

type validatedCSVListValue struct {
	dest      *[]string
	validator func(string) error
	name      string
}

func (v *validatedCSVListValue) String() string {
	if v == nil || v.dest == nil {
		return ""
	}
	return strings.Join(*v.dest, ",")
}

func (v *validatedCSVListValue) Set(s string) error {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return fmt.Errorf("%s cannot be empty", v.name)
	}
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if v.validator != nil {
			if err := v.validator(part); err != nil {
				return err
			}
		}
		*v.dest = append(*v.dest, part)
	}
	return nil
}

func (v *validatedCSVListValue) Type() string { return "strings" }

var envVarNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateTag(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("tag cannot be empty")
	}
	if _, err := reference.ParseNormalizedNamed(trimmed); err != nil {
		return fmt.Errorf("invalid tag %q: %w", trimmed, err)
	}
	return nil
}

func validatePlatform(raw string) error {
	if !strings.Contains(raw, "/") {
		return fmt.Errorf("invalid platform %q (expected os/arch like linux/amd64)", raw)
	}
	if _, err := platforms.Parse(raw); err != nil {
		return fmt.Errorf("invalid platform %q: %w", raw, err)
	}
	return nil
}

func validateEnvVarName(raw string) error {
	if !envVarNameRE.MatchString(raw) {
		return fmt.Errorf("invalid env var name %q", raw)
	}
	return nil
}

func validateSandboxBind(raw string) error {
	host, guest, ok := strings.Cut(raw, ":")
	if !ok {
		return fmt.Errorf("invalid bind %q (expected host:guest)", raw)
	}
	host = strings.TrimSpace(host)
	guest = strings.TrimSpace(guest)
	if host == "" || guest == "" {
		return fmt.Errorf("invalid bind %q (expected host:guest)", raw)
	}
	return nil
}

func validateRemoteAddr(raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("invalid address %q (expected host:port)", raw)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("invalid address %q (host required)", raw)
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("invalid address %q (port required)", raw)
	}
	return nil
}

func validateWSListenAddr(raw string) error {
	if _, err := net.ResolveTCPAddr("tcp", raw); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", raw, err)
	}
	return nil
}

func validateBuildkitAddr(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("builder address cannot be empty")
	}
	if strings.HasPrefix(trimmed, "npipe:") {
		return nil
	}
	if strings.Contains(trimmed, "://") {
		return nil
	}
	return fmt.Errorf("invalid builder address %q (expected a scheme like unix:// or tcp://)", trimmed)
}

func validateRegistryServerArg(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("server cannot be empty")
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("server %q must not contain whitespace", raw)
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid server %q: %w", raw, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("invalid server scheme %q (expected http or https)", u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("invalid server %q (missing host)", raw)
		}
		return nil
	}
	host, port, err := net.SplitHostPort(raw)
	if err == nil {
		if strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
			return fmt.Errorf("invalid server %q", raw)
		}
		return nil
	}
	return nil
}
