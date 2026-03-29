# Crucible IAP — Runner Image
# Ephemeral container that executes infrastructure runs.
# Launched by the worker dispatcher with --read-only, --no-new-privileges, tmpfs /workspace.

FROM alpine:3.21

ARG OPENTOFU_VERSION=1.9.0
ARG TERRAFORM_VERSION=1.10.5
ARG ANSIBLE_VERSION=11.2.0

# Install runtime dependencies
RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    jq \
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

# Non-root user for running jobs
RUN addgroup -S runner && adduser -S -G runner runner

# /workspace is mounted as tmpfs by the dispatcher — ephemeral RAM disk
RUN mkdir -p /workspace && chown runner:runner /workspace

# Copy entrypoint
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

USER runner
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
