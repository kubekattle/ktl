{{/* use the release name as the serviceAccount name for deployment and statefulset collectors */}}
{{- define "alloy.serviceAccountName" -}}
{{- default .Release.Name }}
{{- end }}

{{/* Calculate name of image ID to use for "alloy". */}}
{{- define "alloy.imageId" -}}
{{- printf ":%s" .Chart.AppVersion }}
{{- end }}

{{/* Minimal label helper used by templates in this fixture chart. */}}
{{- define "alloy.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
app.kubernetes.io/name: {{ .Chart.Name | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
{{- end }}
