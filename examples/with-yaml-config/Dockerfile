FROM eventkit

# Copy local config file to the container
COPY config.yaml config.yaml

# Run EventKit with the copied config file
ENTRYPOINT ["/bin/eventkit", "-config", "config.yaml"]
