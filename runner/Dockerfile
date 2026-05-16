# Crucible IAP — Runner Image
# Ephemeral container that executes infrastructure runs.
# Launched by the worker dispatcher with --read-only, --no-new-privileges, tmpfs /workspace.

FROM alpine:3.21@sha256:22e0ec13c0db6b3e1ba3280e831fc50ba7bffe58e81f31670a64b1afede247bc

ARG OPENTOFU_VERSION=1.9.0
ARG TERRAFORM_VERSION=1.10.5
ARG ANSIBLE_VERSION=11.2.0
ARG PULUMI_VERSION=3.154.0
ARG INFRACOST_VERSION=0.10.40
ARG NODE_VERSION=22

# Install runtime dependencies
RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    jq \
    nodejs \
    npm \
    python3 \
    py3-pip \
    openssh-client \
    unzip \
    && rm -rf /var/cache/apk/*

# Install OpenTofu
RUN curl -fsSL "https://github.com/opentofu/opentofu/releases/download/v${OPENTOFU_VERSION}/tofu_${OPENTOFU_VERSION}_linux_amd64.zip" \
    -o /tmp/tofu.zip \
    && unzip /tmp/tofu.zip -d /usr/local/bin/ tofu \
    && rm /tmp/tofu.zip \
    && chmod +x /usr/local/bin/tofu

# Install Terraform (optional; prefer OpenTofu)
RUN curl -fsSL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip" \
    -o /tmp/tf.zip \
    && unzip /tmp/tf.zip -d /usr/local/bin/ \
    && rm /tmp/tf.zip \
    && chmod +x /usr/local/bin/terraform

# Install Ansible
RUN pip3 install --no-cache-dir --break-system-packages ansible==${ANSIBLE_VERSION}

# Install Pulumi CLI
RUN curl -fsSL "https://github.com/pulumi/pulumi/releases/download/v${PULUMI_VERSION}/pulumi-v${PULUMI_VERSION}-linux-x64.tar.gz" \
    | tar -xz --strip-components=1 -C /usr/local/bin/ pulumi/pulumi \
    && chmod +x /usr/local/bin/pulumi

# Install Infracost
RUN curl -fsSL "https://github.com/infracost/infracost/releases/download/v${INFRACOST_VERSION}/infracost-linux-amd64.tar.gz" \
    | tar -xzC /usr/local/bin/ \
    && mv /usr/local/bin/infracost-linux-amd64 /usr/local/bin/infracost \
    && chmod +x /usr/local/bin/infracost

# Non-root user for running jobs
RUN addgroup -S runner && adduser -S -G runner runner

# /workspace is mounted as tmpfs by the dispatcher — ephemeral RAM disk
RUN mkdir -p /workspace && chown runner:runner /workspace

# Copy entrypoint
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

USER runner
WORKDIR /workspace

# Verify the entrypoint is reachable and the bash runtime is intact.
# Interval is long because runners are ephemeral — this is a last-resort
# liveness check, not a readiness probe.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=1 \
  CMD bash -c "test -x /usr/local/bin/entrypoint.sh"

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
