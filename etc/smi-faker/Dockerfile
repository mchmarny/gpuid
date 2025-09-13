FROM cgr.dev/chainguard/bash

# Build arguments for multi-arch support
ARG TARGETPLATFORM
ARG TARGETOS=linux
ARG TARGETARCH

# Move the correct binary based on the target OS and architecture
COPY dist/nvidia-smi-${TARGETOS}-${TARGETARCH} /usr/local/bin/nvidia-smi

# Make it executable
RUN chmod +x /usr/local/bin/nvidia-smi