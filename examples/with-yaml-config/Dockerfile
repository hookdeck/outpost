FROM outpost

# Copy local config file to the container
COPY config.yaml config.yaml

# Run Outpost with the copied config file
ENTRYPOINT ["/bin/outpost", "-config", "config.yaml"]
