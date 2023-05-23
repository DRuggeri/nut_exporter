{{/*
Defining names
*/}}
{{- define "nutexporter.name" -}}
{{- .Release.Name }}-nut-exporter
{{- end }}

{{- define "nutexporter.fullName" -}}
{{- .Release.Namespace }}-{{ include "nutexporter.name" . }}
{{- end }}


{{/*
Common labels
*/}}
{{- define "nutexporter.labels" -}}
{{ include "nutexporter.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: nut-exporter
version: {{ .Chart.Version }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nutexporter.selectorLabels" -}}
app.kubernetes.io/component: server
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/name: nut-exporter
{{- end }}