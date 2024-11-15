{{/*
Service environment variables
*/}}
{{- define "outpost.env" -}}
- name: REDIS_HOST
  value: "{{ .Values.outpost.config.redis.host }}"
- name: REDIS_PORT
  value: "{{ .Values.outpost.config.redis.port }}"
- name: REDIS_PASSWORD
  {{- if ne "" .Values.outpost.config.redis.passwordSecretName }}
  valueFrom:
    secretKeyRef:
      name: "{{ .Values.outpost.config.redis.passwordSecretName }}"
      key: "{{ .Values.outpost.config.redis.passwordSecretKey }}"
  {{- else }}
  value: "{{ .Values.outpost.config.redis.password }}"
  {{- end }}
- name: REDIS_DATABASE
  value: "{{ .Values.outpost.config.redis.database }}"
{{- end }}
