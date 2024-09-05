{{/*
Service environment variables
*/}}
{{- define "eventkit.env" -}}
- name: REDIS_HOST
  value: "{{ .Values.eventkit.config.redis.host }}"
- name: REDIS_PORT
  value: "{{ .Values.eventkit.config.redis.port }}"
- name: REDIS_PASSWORD
  {{- if ne "" .Values.eventkit.config.redis.passwordSecretName }}
  valueFrom:
    secretKeyRef:
      name: "{{ .Values.eventkit.config.redis.passwordSecretName }}"
      key: "{{ .Values.eventkit.config.redis.passwordSecretKey }}"
  {{- else }}
  value: "{{ .Values.eventkit.config.redis.password }}"
  {{- end }}
- name: REDIS_DATABASE
  value: "{{ .Values.eventkit.config.redis.database }}"
{{- end }}
