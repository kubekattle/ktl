// File: internal/api/convert/agent.go
// Brief: Internal convert package implementation for 'agent'.

// Package convert provides convert helpers.

package convert

import (
	"time"

	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/tailer"
	apiv1 "github.com/example/ktl/pkg/api/v1"
)

// ToProtoLogRecord converts a Tailer log record into a protobuf line.
func ToProtoLogRecord(rec tailer.LogRecord) *apiv1.LogLine {
	return &apiv1.LogLine{
		TimestampUnixNano:  rec.Timestamp.UnixNano(),
		FormattedTimestamp: rec.FormattedTimestamp,
		Namespace:          rec.Namespace,
		Pod:                rec.Pod,
		Container:          rec.Container,
		Raw:                rec.Raw,
		Rendered:           rec.Rendered,
		Source:             rec.Source,
		SourceGlyph:        rec.SourceGlyph,
		RenderedEqualsRaw:  rec.RenderedEqualsRaw,
	}
}

// FromProtoLogLine converts a protobuf log line into a Tailer record.
func FromProtoLogLine(line *apiv1.LogLine) tailer.LogRecord {
	if line == nil {
		return tailer.LogRecord{}
	}
	var ts time.Time
	if line.TimestampUnixNano != 0 {
		ts = time.Unix(0, line.TimestampUnixNano)
	}
	return tailer.LogRecord{
		Timestamp:          ts,
		FormattedTimestamp: line.FormattedTimestamp,
		Namespace:          line.Namespace,
		Pod:                line.Pod,
		Container:          line.Container,
		Raw:                line.Raw,
		Rendered:           line.Rendered,
		Source:             line.Source,
		SourceGlyph:        line.SourceGlyph,
		RenderedEqualsRaw:  line.RenderedEqualsRaw,
	}
}

// DefaultConfigFromProto hydrates a config.Options from a protobuf request.
func DefaultConfigFromProto(req *apiv1.LogRequest) *config.Options {
	opts := config.NewOptions()
	if req == nil {
		return opts
	}
	opts.PodQuery = req.GetPodQuery()
	opts.Namespaces = append([]string(nil), req.GetNamespaces()...)
	opts.AllNamespaces = req.GetAllNamespaces()
	opts.LabelSelector = req.GetLabelSelector()
	opts.FieldSelector = req.GetFieldSelector()
	opts.ContainerFilters = append([]string(nil), req.GetContainers()...)
	opts.ExcludeContainers = append([]string(nil), req.GetExcludeContainers()...)
	opts.ExcludePods = append([]string(nil), req.GetExcludePods()...)
	opts.HighlightTerms = append([]string(nil), req.GetHighlightTerms()...)
	opts.Events = req.GetIncludeEvents()
	opts.EventsOnly = req.GetEventsOnly()
	if req.GetTailLines() != 0 {
		opts.TailLines = req.GetTailLines()
	}
	opts.Follow = req.GetFollow()
	opts.ShowTimestamp = req.GetTimestamps()
	if tmpl := req.GetTemplate(); tmpl != "" {
		opts.Template = tmpl
	}
	opts.KubeConfigPath = req.GetKubeconfigPath()
	opts.Context = req.GetKubeContext()
	return opts
}

// LogOptionsToProto converts CLI log options into a protobuf request.
func LogOptionsToProto(opts *config.Options) *apiv1.LogRequest {
	if opts == nil {
		return &apiv1.LogRequest{}
	}
	return &apiv1.LogRequest{
		PodQuery:          opts.PodQuery,
		Namespaces:        append([]string(nil), opts.Namespaces...),
		AllNamespaces:     opts.AllNamespaces,
		LabelSelector:     opts.LabelSelector,
		FieldSelector:     opts.FieldSelector,
		Containers:        append([]string(nil), opts.ContainerFilters...),
		ExcludePods:       append([]string(nil), opts.ExcludePods...),
		ExcludeContainers: append([]string(nil), opts.ExcludeContainers...),
		HighlightTerms:    append([]string(nil), opts.HighlightTerms...),
		IncludeEvents:     opts.Events,
		EventsOnly:        opts.EventsOnly,
		TailLines:         opts.TailLines,
		Follow:            opts.Follow,
		Timestamps:        opts.ShowTimestamp,
		Template:          opts.Template,
		KubeContext:       opts.Context,
		KubeconfigPath:    opts.KubeConfigPath,
	}
}
