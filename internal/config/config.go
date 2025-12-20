// File: internal/config/config.go
// Brief: Internal config package implementation for 'config'.

// Package config defines the flag plumbing and runtime options shared by ktl's
// logging commands, translating Cobra/Viper flag values into a strongly typed
// struct that the tailer and capture pipelines consume.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
)

// Options holds all CLI configuration used by the tailer.
type Options struct {
	PodQuery              string
	Namespaces            []string
	AllNamespaces         bool
	LabelSelector         string
	FieldSelector         string
	ContainerFilters      []string
	ExcludeContainers     []string
	ExcludePods           []string
	ExcludeLine           string
	HighlightTerms        []string
	DiffContainer         bool
	Follow                bool
	NoFollow              bool
	Since                 time.Duration
	SinceRaw              string
	TailLines             int64
	ShowTimestamp         bool
	TimestampFormat       string
	Template              string
	TemplateFile          string
	OnlyLogLines          bool
	NoPrefix              bool
	PlainOutput           bool
	JSONOutput            bool
	ColorMode             string
	PodColorStrings       []string
	ContainerColorStrings []string
	OutputFormat          string
	Events                bool
	EventsOnly            bool
	KubeConfigPath        string
	Context               string
	Stdin                 bool
	ContainerRegex        []*regexp.Regexp
	ExcludeRegex          []*regexp.Regexp
	ExcludeLineRegex      *regexp.Regexp
	ExcludePodRegex       []*regexp.Regexp
	SearchRegex           []*regexp.Regexp
	TimeZone              string
	TimeLocation          *time.Location
	ConditionArgs         []string
	ConditionFilters      map[corev1.PodConditionType]corev1.ConditionStatus
	NodeLogs              bool
	NodeLogFiles          []string
	NodeLogAll            bool
	NodeLogsOnly          bool
	WSListenAddr          string
}

const defaultTemplate = "[{{.Timestamp}}] {{.PodDisplay}} {{.ContainerTag}} {{.Message}}"
const rawTemplate = "{{.Raw}}"
const jsonTemplate = "{{printf \"{\\\"timestamp\\\":\\\"%s\\\",\\\"namespace\\\":\\\"%s\\\",\\\"pod\\\":\\\"%s\\\",\\\"container\\\":\\\"%s\\\",\\\"message\\\":%q}\" .Timestamp .Namespace .PodName .ContainerName .Message}}"
const extJSONTemplate = "{{printf \"{\\\"timestamp\\\":\\\"%s\\\",\\\"namespace\\\":\\\"%s\\\",\\\"pod\\\":\\\"%s\\\",\\\"container\\\":\\\"%s\\\",\\\"message\\\":%q,\\\"raw\\\":%q}\" .Timestamp .Namespace .PodName .ContainerName .Message .Raw}}"
const ppExtJSONTemplate = "{{printf \"{\\\"ts\\\":\\\"%s\\\",\\\"ns\\\":\\\"%s\\\",\\\"pod_name\\\":\\\"%s\\\",\\\"container_name\\\":\\\"%s\\\",\\\"msg\\\":%q,\\\"raw\\\":%q}\" .Timestamp .Namespace .PodName .ContainerName .Message .Raw}}"
const defaultNodeLogFile = "kubelet.log"

// DefaultTemplate exposes the default log template so other packages can compare against it safely.
func DefaultTemplate() string {
	return defaultTemplate
}

const TimestampFormatYouTube = "youtube"

var conditionAliases = map[string]corev1.PodConditionType{
	"ready":            corev1.PodReady,
	"podready":         corev1.PodReady,
	"initialized":      corev1.PodInitialized,
	"podinitialized":   corev1.PodInitialized,
	"scheduled":        corev1.PodScheduled,
	"podscheduled":     corev1.PodScheduled,
	"containersready":  corev1.ContainersReady,
	"containers-ready": corev1.ContainersReady,
}

// NewOptions returns Options with defaults applied.
func NewOptions() *Options {
	return &Options{
		Follow:          true,
		TailLines:       10,
		ShowTimestamp:   true,
		TimestampFormat: TimestampFormatYouTube,
		Template:        defaultTemplate,
		ColorMode:       "auto",
		OutputFormat:    "default",
	}
}

// AddFlags binds configuration flags to the provided Cobra command.
func (o *Options) AddFlags(cmd *cobra.Command) {
	o.BindFlags(cmd.Flags())
}

// BindFlags attaches log flags to an arbitrary FlagSet and returns the flag names for further customization.
func (o *Options) BindFlags(fs *pflag.FlagSet) []string {
	var names []string
	fs.BoolVarP(&o.AllNamespaces, "all-namespaces", "A", false, "If present, tail across all namespaces (overrides --namespace)")
	names = append(names, "all-namespaces")
	fs.StringSliceVarP(&o.Namespaces, "namespace", "n", nil, "Kubernetes namespace to use. Defaults to the context namespace; repeat or use comma-separated values for multiple.")
	names = append(names, "namespace")
	fs.StringVarP(&o.LabelSelector, "selector", "l", "", "Label selector to filter pods")
	names = append(names, "selector")
	fs.StringSliceVarP(&o.ContainerFilters, "container", "c", nil, "Regex filter for container names (repeat to OR multiple)")
	names = append(names, "container")
	fs.StringSliceVarP(&o.ExcludeContainers, "exclude-container", "C", nil, "Regex for container names to exclude")
	names = append(names, "exclude-container")
	fs.StringSliceVar(&o.ExcludePods, "exclude-pod", nil, "Regex for pod names to exclude")
	names = append(names, "exclude-pod")
	fs.StringVarP(&o.ExcludeLine, "exclude", "x", "", "Regex to skip log lines that match")
	names = append(names, "exclude")
	fs.StringArrayVarP(&o.HighlightTerms, "highlight", "H", nil, "Log lines to highlight (regular expression)")
	names = append(names, "highlight")
	fs.StringArrayVar(&o.ConditionArgs, "condition", nil, "Filter pods by condition, e.g. ready=false")
	names = append(names, "condition")
	fs.BoolVarP(&o.Follow, "follow", "f", true, "Follow log output")
	names = append(names, "follow")
	fs.BoolVar(&o.NoFollow, "no-follow", false, "Alias for --follow=false")
	names = append(names, "no-follow")
	fs.BoolVarP(&o.DiffContainer, "diff-container", "d", false, "Display different colors for different containers")
	names = append(names, "diff-container")
	fs.StringVarP(&o.SinceRaw, "since", "s", "", "Return logs newer than a relative duration like 5s, 2m, or 3h")
	names = append(names, "since")
	if flag := fs.Lookup("since"); flag != nil {
		flag.NoOptDefVal = "0s"
	}
	fs.Int64VarP(&o.TailLines, "tail", "t", 10, "Number of historic log lines to show, -1 for all available")
	names = append(names, "tail")
	if tailFlag := fs.Lookup("tail"); tailFlag != nil {
		tailFlag.NoOptDefVal = fmt.Sprintf("%d", o.TailLines)
	}
	fs.BoolVarP(&o.ShowTimestamp, "timestamps", "T", true, "Show timestamps in output")
	names = append(names, "timestamps")
	fs.StringVarP(&o.TimestampFormat, "timestamp-format", "F", TimestampFormatYouTube, "Go time format string for timestamps (use \"youtube\" for H:MM:SS)")
	names = append(names, "timestamp-format")
	fs.StringVarP(&o.Template, "template", "p", defaultTemplate, "Go template for log lines; available fields: Timestamp, Namespace, PodName, ContainerName, Message, Raw")
	names = append(names, "template")
	fs.StringVar(&o.TemplateFile, "template-file", "", "Path to a Go template file for log output")
	names = append(names, "template-file")
	fs.BoolVar(&o.OnlyLogLines, "only-log-lines", false, "Print only the log message body (no timestamps or prefixes)")
	names = append(names, "only-log-lines")
	fs.StringVarP(&o.OutputFormat, "output", "o", "default", "Specify predefined template: default, raw, json, extjson, ppextjson")
	names = append(names, "output")
	fs.BoolVarP(&o.NoPrefix, "no-prefix", "P", false, "Disable default prefix (overrides template to just message)")
	names = append(names, "no-prefix")
	fs.BoolVar(&o.PlainOutput, "plain", false, "Shorthand for --only-log-lines and --no-prefix")
	names = append(names, "plain")
	fs.BoolVarP(&o.JSONOutput, "json", "j", false, "Emit raw JSON log lines without decoration")
	names = append(names, "json")
	fs.StringVarP(&o.ColorMode, "color", "m", "auto", "Force set color output. 'auto': colorize if tty attached, 'always': always colorize, 'never': never colorize")
	names = append(names, "color")
	fs.StringSliceVarP(&o.PodColorStrings, "pod-colors", "g", nil, "Comma-separated SGR color codes used to color pod names (e.g. \"91,92,93\")")
	names = append(names, "pod-colors")
	fs.StringSliceVar(&o.ContainerColorStrings, "container-colors", nil, "Comma-separated SGR color codes used to color container names")
	names = append(names, "container-colors")
	fs.BoolVarP(&o.Events, "events", "e", false, "Include Kubernetes events that match the pod selector")
	names = append(names, "events")
	fs.BoolVarP(&o.EventsOnly, "events-only", "O", false, "Only stream matching events (implies --events)")
	names = append(names, "events-only")
	fs.StringVar(&o.FieldSelector, "field-selector", "", "Field selector to filter pods (e.g. spec.nodeName=kind-control-plane)")
	names = append(names, "field-selector")
	fs.StringVar(&o.TimeZone, "timezone", "", "IANA timezone name used when rendering timestamps (e.g. Asia/Tokyo)")
	names = append(names, "timezone")
	fs.BoolVar(&o.Stdin, "stdin", false, "Read log lines from STDIN instead of Kubernetes")
	names = append(names, "stdin")
	fs.BoolVar(&o.NodeLogs, "node-logs", false, "Also stream node/system logs (defaults to kubelet.log) from nodes hosting matched pods")
	names = append(names, "node-logs")
	fs.StringSliceVar(&o.NodeLogFiles, "node-log", nil, "Specific node/system log files (relative to /var/log) to stream via the kubelet proxy; repeat for multiple")
	names = append(names, "node-log")
	fs.BoolVar(&o.NodeLogAll, "node-log-all", false, "Stream node logs from every node instead of only those hosting matched pods")
	names = append(names, "node-log-all")
	fs.BoolVar(&o.NodeLogsOnly, "node-log-only", false, "Suppress pod logs and stream only node/system logs")
	names = append(names, "node-log-only")
	fs.StringVar(&o.WSListenAddr, "ws-listen", "", "Expose a raw WebSocket log feed at this address (e.g. :9090)")
	names = append(names, "ws-listen")
	return names
}

// Validate ensures provided options are coherent and compiles regex inputs.
func (o *Options) Validate() error {
	if o.PodQuery == "" {
		o.PodQuery = ".*"
	}
	if strings.Contains(o.PodQuery, "/") {
		parts := strings.SplitN(o.PodQuery, "/", 2)
		if len(parts) == 2 && parts[1] != "" {
			namespaceHint := strings.TrimSpace(parts[0])
			o.PodQuery = parts[1]
			if namespaceHint != "" && len(o.Namespaces) == 0 && !o.AllNamespaces {
				o.Namespaces = []string{namespaceHint}
			}
		}
	}
	if _, err := regexp.Compile(o.PodQuery); err != nil {
		return fmt.Errorf("invalid pod regex %q: %w", o.PodQuery, err)
	}
	for _, val := range o.ContainerFilters {
		re, err := regexp.Compile(val)
		if err != nil {
			return fmt.Errorf("invalid container regex %q: %w", val, err)
		}
		o.ContainerRegex = append(o.ContainerRegex, re)
	}
	for _, val := range o.ExcludeContainers {
		re, err := regexp.Compile(val)
		if err != nil {
			return fmt.Errorf("invalid exclude-container regex %q: %w", val, err)
		}
		o.ExcludeRegex = append(o.ExcludeRegex, re)
	}
	for _, val := range o.ExcludePods {
		re, err := regexp.Compile(val)
		if err != nil {
			return fmt.Errorf("invalid exclude-pod regex %q: %w", val, err)
		}
		o.ExcludePodRegex = append(o.ExcludePodRegex, re)
	}
	if o.ExcludeLine != "" {
		re, err := regexp.Compile(o.ExcludeLine)
		if err != nil {
			return fmt.Errorf("invalid exclude regex %q: %w", o.ExcludeLine, err)
		}
		o.ExcludeLineRegex = re
	}
	for _, val := range o.HighlightTerms {
		re, err := regexp.Compile(val)
		if err != nil {
			return fmt.Errorf("invalid search regex %q: %w", val, err)
		}
		o.SearchRegex = append(o.SearchRegex, re)
	}
	if o.SinceRaw != "" {
		dur, err := time.ParseDuration(o.SinceRaw)
		if err != nil {
			return fmt.Errorf("invalid since duration %q: %w", o.SinceRaw, err)
		}
		o.Since = dur
	}
	if o.TailLines < -1 {
		return fmt.Errorf("--tail cannot be less than -1")
	}
	if strings.TrimSpace(o.TemplateFile) != "" {
		data, err := os.ReadFile(o.TemplateFile)
		if err != nil {
			return fmt.Errorf("read template file %q: %w", o.TemplateFile, err)
		}
		o.Template = string(data)
	}
	if len(o.ConditionArgs) > 0 {
		o.ConditionFilters = make(map[corev1.PodConditionType]corev1.ConditionStatus)
		for _, arg := range o.ConditionArgs {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid --condition value %q (expected key=value)", arg)
			}
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.ToLower(strings.TrimSpace(parts[1]))
			condType, ok := conditionAliases[key]
			if !ok {
				return fmt.Errorf("unknown condition %q (supported: ready, initialized, containersready, scheduled)", key)
			}
			status, err := parseConditionStatus(val)
			if err != nil {
				return err
			}
			o.ConditionFilters[condType] = status
		}
	}
	if o.NoFollow {
		o.Follow = false
	}
	if o.PlainOutput {
		o.OnlyLogLines = true
		o.NoPrefix = true
	}
	if o.OnlyLogLines {
		o.ShowTimestamp = false
		o.NoPrefix = true
		o.Template = "{{.Message}}"
	}
	if o.NoPrefix {
		o.Template = "{{.Message}}"
	}
	if o.JSONOutput {
		o.NoPrefix = true
		o.ShowTimestamp = false
	}
	if o.EventsOnly {
		o.Events = true
	}
	if err := o.applyOutputFormat(); err != nil {
		return err
	}
	switch strings.ToLower(o.ColorMode) {
	case "", "auto":
		o.ColorMode = "auto"
	case "always":
		o.ColorMode = "always"
	case "never":
		o.ColorMode = "never"
	default:
		return fmt.Errorf("invalid --color value %q (allowed: auto, always, never)", o.ColorMode)
	}
	if o.AllNamespaces && len(o.Namespaces) > 0 {
		return fmt.Errorf("cannot combine --all-namespaces with explicit --namespace")
	}
	for idx, ns := range o.Namespaces {
		o.Namespaces[idx] = strings.TrimSpace(ns)
	}
	o.FieldSelector = strings.TrimSpace(o.FieldSelector)
	if tz := strings.TrimSpace(o.TimeZone); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return fmt.Errorf("invalid --timezone value %q: %w", o.TimeZone, err)
		}
		o.TimeZone = tz
		o.TimeLocation = loc
	}
	if o.NodeLogsOnly {
		o.NodeLogs = true
	}
	if err := o.compileNodeLogConfig(); err != nil {
		return err
	}
	return nil
}

func (o *Options) compileNodeLogConfig() error {
	if !o.NodeLogs && len(o.NodeLogFiles) == 0 {
		return nil
	}
	if o.NodeLogs && len(o.NodeLogFiles) == 0 {
		o.NodeLogFiles = []string{defaultNodeLogFile}
	}
	clean := make([]string, 0, len(o.NodeLogFiles))
	seen := make(map[string]struct{}, len(o.NodeLogFiles))
	for _, entry := range o.NodeLogFiles {
		trimmed := strings.TrimSpace(entry)
		trimmed = strings.TrimPrefix(trimmed, "/")
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "..") {
			return fmt.Errorf("node log path %q cannot contain '..'", entry)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		clean = append(clean, trimmed)
	}
	if len(clean) == 0 {
		o.NodeLogFiles = nil
	}
	return nil
}

func (o *Options) applyOutputFormat() error {
	mode := strings.ToLower(strings.TrimSpace(o.OutputFormat))
	if mode == "" {
		mode = "default"
	}
	switch mode {
	case "default":
		o.OutputFormat = "default"
		return nil
	case "raw":
		o.Template = rawTemplate
	case "json":
		o.Template = jsonTemplate
	case "extjson":
		o.Template = extJSONTemplate
	case "ppextjson":
		o.Template = ppExtJSONTemplate
	default:
		return fmt.Errorf("invalid --output value %q (must be one of: default, raw, json, extjson, ppextjson)", o.OutputFormat)
	}
	o.OutputFormat = mode
	return nil
}

func parseConditionStatus(val string) (corev1.ConditionStatus, error) {
	switch val {
	case "true", "1", "ready", "yes", "y":
		return corev1.ConditionTrue, nil
	case "false", "0", "no", "n":
		return corev1.ConditionFalse, nil
	case "unknown":
		return corev1.ConditionUnknown, nil
	default:
		return corev1.ConditionFalse, fmt.Errorf("invalid condition status %q (use true/false/unknown)", val)
	}
}
