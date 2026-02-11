{{- define "ktl-applyplan-e2e.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ktl-applyplan-e2e.fullname" -}}
{{- $name := include "ktl-applyplan-e2e.name" . -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

