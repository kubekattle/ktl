{{- define "verify-smoke.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "verify-smoke.fullname" -}}
{{- printf "%s-%s" (include "verify-smoke.name" .) .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
