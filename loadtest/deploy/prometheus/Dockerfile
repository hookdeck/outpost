# Prometheus for benchmark runs against a deployed loadtest app.
#
# The config is baked in rather than mounted: Railway has no config-file
# primitive, and a volume would make the config something you edit by hand on
# a running service instead of something the repo records.
#
#   railway up   # from this directory, against the loadtest project
FROM prom/prometheus:v3.13.1

COPY prometheus.yml /etc/prometheus/prometheus.yml

# --enable-feature=native-histograms is the third of the three settings native
# histograms need; the other two are in prometheus.yml. Missing any one of them
# degrades to classic buckets silently, and fetch.py's queries return empty.
#
# No volume, so the TSDB dies with the container. That is fine for this job:
# bench.sh folds the series into the run export the moment the run ends, and
# the export is the artifact. Nothing reads Prometheus afterwards.
# --web.enable-otlp-receiver opens /api/v1/otlp/v1/metrics so Outpost can push
# its own metrics in. Outpost has no Prometheus endpoint to scrape — it only
# exports OTLP — and it runs in a different Railway project, so private
# networking cannot reach this service and the push arrives over the public
# domain. That makes this write path unauthenticated and open to anyone who
# knows the URL; see prometheus.yml for how the two sources are kept apart.
CMD [ "--config.file=/etc/prometheus/prometheus.yml", \
      "--storage.tsdb.path=/prometheus", \
      "--storage.tsdb.retention.time=7d", \
      "--enable-feature=native-histograms", \
      "--web.enable-otlp-receiver", \
      "--web.listen-address=:9090" ]
