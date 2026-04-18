#!/usr/bin/env bash
# Crucible IAP — Runner Entrypoint
# Executed inside the ephemeral job container.
# All env vars are injected by the dispatcher at container spawn time.
set -euo pipefail

log()  { echo "[crucible-runner] $*" >&2; }
fail() { log "ERROR: $*"; exit 1; }

# ── Validate required environment ────────────────────────────────────────────
: "${CRUCIBLE_RUN_ID:?CRUCIBLE_RUN_ID is required}"
: "${CRUCIBLE_STACK_ID:?CRUCIBLE_STACK_ID is required}"
: "${CRUCIBLE_API_URL:?CRUCIBLE_API_URL is required}"
: "${CRUCIBLE_JOB_TOKEN:?CRUCIBLE_JOB_TOKEN is required}"
: "${CRUCIBLE_TOOL:?CRUCIBLE_TOOL is required}"
: "${CRUCIBLE_REPO_URL:?CRUCIBLE_REPO_URL is required}"
CRUCIBLE_REPO_BRANCH="${CRUCIBLE_REPO_BRANCH:-main}"
CRUCIBLE_PROJECT_ROOT="${CRUCIBLE_PROJECT_ROOT:-.}"
CRUCIBLE_RUN_TYPE="${CRUCIBLE_RUN_TYPE:-tracked}"
CRUCIBLE_VCS_TOKEN="${CRUCIBLE_VCS_TOKEN:-}"

API_HEADERS=(
    -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}"
    -H "Content-Type: application/json"
)

# ── Helper: report intermediate status ───────────────────────────────────────
report_status() {
    local status="$1"
    log "reporting status: ${status}"
    curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/status" \
        "${API_HEADERS[@]}" \
        -d "{\"status\":\"${status}\"}" \
        || log "warn: failed to report status ${status}"
}

# ── Helper: upload plan artifact ─────────────────────────────────────────────
upload_plan() {
    local plan_file="$1"
    if [[ -f "${plan_file}" ]]; then
        log "uploading plan artifact"
        curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan" \
            -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
            -H "Content-Type: application/octet-stream" \
            --data-binary "@${plan_file}" \
            || log "warn: plan upload failed"
    fi
}

# ── Provider cache (OpenTofu / Terraform) ────────────────────────────────────
# Providers are stored in MinIO keyed by their path relative to TF_PLUGIN_CACHE_DIR.
# Each run restores cached providers before init (so init skips registry downloads)
# and uploads any newly-downloaded providers after init.

PROVIDER_CACHE_DIR="/workspace/.provider-cache"
# Keys fetched during restore are reused by save to avoid a second round-trip.
_PROVIDER_CACHE_KEYS=""

restore_provider_cache() {
    log "restoring provider cache"
    mkdir -p "${PROVIDER_CACHE_DIR}"

    local platform
    platform="$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"

    _PROVIDER_CACHE_KEYS=$(curl -sf \
        -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
        "${CRUCIBLE_API_URL}/api/v1/internal/provider-cache?platform=${platform}" \
        | jq -r '.keys[]') || { log "warn: could not fetch provider cache list — skipping restore"; return 0; }

    local count=0
    while IFS= read -r key; do
        local dest="${PROVIDER_CACHE_DIR}/${key}"
        if [[ ! -f "$dest" ]]; then
            mkdir -p "$(dirname "$dest")"
            if curl -sf \
                -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
                "${CRUCIBLE_API_URL}/api/v1/internal/provider-cache/${key}" \
                -o "$dest"; then
                chmod +x "$dest"
                count=$(( count + 1 ))
            else
                log "warn: failed to download provider ${key} — will download from registry"
                rm -f "$dest"
            fi
        fi
    done <<< "${_PROVIDER_CACHE_KEYS}"

    log "provider cache: restored ${count} provider(s) from cache"
}

save_provider_cache() {
    [[ -d "${PROVIDER_CACHE_DIR}" ]] || return 0
    log "updating provider cache"

    local count=0
    while IFS= read -r file; do
        local key="${file#"${PROVIDER_CACHE_DIR}"/}"
        if ! grep -qxF "$key" <<< "${_PROVIDER_CACHE_KEYS}"; then
            if curl -sf -X PUT \
                -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
                -H "Content-Type: application/octet-stream" \
                --data-binary "@${file}" \
                "${CRUCIBLE_API_URL}/api/v1/internal/provider-cache/${key}"; then
                count=$(( count + 1 ))
            else
                log "warn: failed to cache provider ${key}"
            fi
        fi
    done < <(find "${PROVIDER_CACHE_DIR}" -type f)

    log "provider cache: uploaded ${count} new provider(s)"
}

# ── OpenTofu / Terraform shared logic ────────────────────────────────────────
run_tf_generic() {
    local bin="$1"
    log "tool=${bin} run_type=${CRUCIBLE_RUN_TYPE}"
    log "version: $(${bin} version -json 2>/dev/null | grep -o '"terraform_version":"[^"]*"' | cut -d'"' -f4 || ${bin} version 2>&1 | head -1)"

    # Point Terraform HTTP backend at Crucible's state API.
    # Runner JWT is accepted as password (aud=runner, stack_id claim validated).
    export TF_HTTP_ADDRESS="${CRUCIBLE_API_URL}/api/v1/state/${CRUCIBLE_STACK_ID}"
    export TF_HTTP_LOCK_ADDRESS="${TF_HTTP_ADDRESS}"
    export TF_HTTP_UNLOCK_ADDRESS="${TF_HTTP_ADDRESS}"
    export TF_HTTP_USERNAME="${CRUCIBLE_STACK_ID}"
    export TF_HTTP_PASSWORD="${CRUCIBLE_JOB_TOKEN}"
    export TF_IN_AUTOMATION=1
    export TF_INPUT=0

    log "state backend: ${TF_HTTP_ADDRESS}"

    # Verify API reachability and auth before handing off to OpenTofu.
    # This surfaces the real HTTP error instead of go-retryablehttp's
    # opaque "giving up after N attempts" message.
    log "checking state backend connectivity..."
    state_check=$(curl -sf -o /dev/null -w "%{http_code}" \
        -u "${TF_HTTP_USERNAME}:${TF_HTTP_PASSWORD}" \
        "${TF_HTTP_ADDRESS}" 2>&1) || state_check="$?"
    case "${state_check}" in
        200|204) log "state backend: ok (${state_check})" ;;
        401|403) fail "state backend auth failed (${state_check}) — check CRUCIBLE_SECRET_KEY matches between API and runner config" ;;
        000)     fail "state backend unreachable — cannot connect to ${TF_HTTP_ADDRESS} (DNS or network issue)" ;;
        *)       log "state backend returned ${state_check} — continuing (empty state is normal for first run)" ;;
    esac

    export TF_PLUGIN_CACHE_DIR="${PROVIDER_CACHE_DIR}"
    restore_provider_cache

    log "initialising"
    ${bin} init -no-color

    save_provider_cache

    case "${CRUCIBLE_RUN_TYPE}" in
        destroy)
            log "planning destroy"
            report_status "planning"
            ${bin} plan -no-color -destroy -out=/workspace/plan.tfplan
            upload_plan /workspace/plan.tfplan
            if [[ "${CRUCIBLE_RUN_TYPE}" == "destroy" ]]; then
                # Destroy runs always require explicit confirmation — never auto-apply.
                log "plan complete — awaiting confirmation before destroy"
            fi
            ;;

        apply)
            # Second phase: a confirmed run re-enqueues with run_type=apply.
            # Download the saved plan and apply it.
            log "applying saved plan"
            report_status "applying"
            curl -sf "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan" \
                -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
                -o /workspace/plan.tfplan \
                || fail "failed to download plan artifact"
            ${bin} apply -no-color /workspace/plan.tfplan
            ;;

        proposed)
            # Plan only — no apply, no confirmation needed.
            log "running plan (proposed)"
            report_status "planning"
            ${bin} plan -no-color -out=/workspace/plan.tfplan
            upload_plan /workspace/plan.tfplan
            ;;

        tracked|*)
            # Default: plan, upload, then wait for human confirmation.
            log "running plan"
            report_status "planning"
            ${bin} plan -no-color -out=/workspace/plan.tfplan
            upload_plan /workspace/plan.tfplan
            log "plan complete — awaiting confirmation"
            ;;
    esac
}

run_opentofu()  { run_tf_generic "tofu"; }
run_terraform() { run_tf_generic "terraform"; }

# ── Ansible ───────────────────────────────────────────────────────────────────

# Parse the PLAY RECAP block to extract aggregate change/failure counts.
# Ansible recap format per host:
#   hostname : ok=N  changed=N  unreachable=N  failed=N  skipped=N  ...
# We sum changed across all hosts and report it as the "change" count.
# Unreachable hosts are mapped to "destroy" (closest semantic for "lost").
report_ansible_summary() {
    local output_file="$1"
    local changed=0 failed=0 unreachable=0

    while IFS= read -r line; do
        if [[ "${line}" =~ changed=([0-9]+) ]];     then changed=$(( changed + BASH_REMATCH[1] )); fi
        if [[ "${line}" =~ failed=([0-9]+) ]];      then failed=$(( failed + BASH_REMATCH[1] )); fi
        if [[ "${line}" =~ unreachable=([0-9]+) ]]; then unreachable=$(( unreachable + BASH_REMATCH[1] )); fi
    done < "${output_file}"

    log "ansible summary: changed=${changed} failed=${failed} unreachable=${unreachable}"
    curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan-summary" \
        "${API_HEADERS[@]}" \
        -d "{\"add\":0,\"change\":${changed},\"destroy\":${unreachable}}" \
        || log "warn: failed to report plan summary"
}

run_ansible() {
    local playbook="${CRUCIBLE_ANSIBLE_PLAYBOOK:-site.yml}"
    log "tool=ansible run_type=${CRUCIBLE_RUN_TYPE}"
    log "version: $(ansible --version 2>&1 | head -1)"

    # Ephemeral containers have no persistent known_hosts — disable host key
    # checking so SSH-based playbooks don't stall waiting for user input.
    export ANSIBLE_HOST_KEY_CHECKING=False
    export ANSIBLE_FORCE_COLOR=True

    # Resolve inventory: explicit env var > auto-detect common repo paths > ansible defaults.
    local inv_args=()
    if [[ -n "${CRUCIBLE_ANSIBLE_INVENTORY:-}" ]]; then
        inv_args=(-i "${CRUCIBLE_ANSIBLE_INVENTORY}")
        log "inventory: ${CRUCIBLE_ANSIBLE_INVENTORY} (from CRUCIBLE_ANSIBLE_INVENTORY)"
    else
        for candidate in inventory.ini inventory.yml inventory.yaml inventory hosts hosts.ini; do
            if [[ -e "${candidate}" ]]; then
                inv_args=(-i "${candidate}")
                log "inventory: ${candidate} (auto-detected)"
                break
            fi
        done
        [[ ${#inv_args[@]} -gt 0 ]] || log "inventory: none found — using ansible defaults (implicit localhost)"
    fi

    case "${CRUCIBLE_RUN_TYPE}" in
        apply)
            # Second phase: download the check output for audit, then run the playbook for real.
            log "applying playbook: ${playbook}"
            report_status "applying"
            # Download the saved check output (best-effort — non-fatal if unavailable).
            curl -sf "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan" \
                -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
                -o /workspace/ansible_check.txt \
                || log "warn: could not retrieve check artifact (non-fatal)"
            ansible-playbook "${playbook}" "${inv_args[@]}"
            ;;

        destroy)
            # Ansible has no built-in destroy semantics. A separate teardown
            # playbook must be provided via CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK.
            local teardown="${CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK:-}"
            [[ -n "${teardown}" ]] \
                || fail "destroy runs require CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK — Ansible has no built-in destroy operation"
            log "running destroy check: ${teardown}"
            report_status "planning"
            ansible-playbook "${teardown}" --check --diff "${inv_args[@]}" \
                2>&1 | tee /workspace/ansible_check.txt || {
                upload_plan /workspace/ansible_check.txt
                report_ansible_summary /workspace/ansible_check.txt
                fail "ansible destroy check failed"
            }
            upload_plan /workspace/ansible_check.txt
            report_ansible_summary /workspace/ansible_check.txt
            log "destroy check complete — awaiting confirmation"
            ;;

        proposed|tracked|*)
            # Check mode: preview changes without applying.
            # Output is captured and uploaded as the plan artifact so the UI
            # can display the diff and the operator can confirm before applying.
            log "running check: ${playbook}"
            report_status "planning"
            ansible-playbook "${playbook}" --check --diff "${inv_args[@]}" \
                2>&1 | tee /workspace/ansible_check.txt || {
                upload_plan /workspace/ansible_check.txt
                report_ansible_summary /workspace/ansible_check.txt
                fail "ansible-playbook --check failed"
            }
            upload_plan /workspace/ansible_check.txt
            report_ansible_summary /workspace/ansible_check.txt
            if [[ "${CRUCIBLE_RUN_TYPE}" == "proposed" ]]; then
                log "check complete (proposed — no apply)"
            else
                log "check complete — awaiting confirmation"
            fi
            ;;
    esac
}

# ── Pulumi ────────────────────────────────────────────────────────────────────

# Parse `pulumi preview --json` output and report the change summary to Crucible.
report_pulumi_summary() {
    local json_file="$1"
    local add=0 change=0 destroy=0
    if [[ -f "${json_file}" ]]; then
        add=$(jq -r '.changeSummary.create // 0' "${json_file}" 2>/dev/null || echo 0)
        change=$(jq -r '.changeSummary.update // 0' "${json_file}" 2>/dev/null || echo 0)
        destroy=$(jq -r '.changeSummary.delete // 0' "${json_file}" 2>/dev/null || echo 0)
    fi
    log "pulumi summary: create=${add} update=${change} delete=${destroy}"
    curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan-summary" \
        "${API_HEADERS[@]}" \
        -d "{\"add\":${add},\"change\":${change},\"destroy\":${destroy}}" \
        || log "warn: failed to report plan summary"
}

run_pulumi() {
    local stack="${CRUCIBLE_PULUMI_STACK:-crucible-${CRUCIBLE_STACK_ID}}"
    log "tool=pulumi run_type=${CRUCIBLE_RUN_TYPE}"
    log "version: $(pulumi version 2>&1)"

    # State passphrase is required — Pulumi encrypts stack config and secrets.
    [[ -n "${PULUMI_CONFIG_PASSPHRASE:-}" ]] \
        || fail "PULUMI_CONFIG_PASSPHRASE is required for Pulumi stacks — set it as a secret env var on this stack"

    # Store Pulumi's home directory on the tmpfs workspace so it can write
    # language plugins and credentials without hitting the read-only rootfs.
    export PULUMI_HOME=/workspace/.pulumi
    export PULUMI_SKIP_UPDATE_CHECK=1

    # Configure the DIY S3 backend unless the user has already set PULUMI_BACKEND_URL.
    if [[ -z "${PULUMI_BACKEND_URL:-}" ]]; then
        local proto="http"
        [[ "${CRUCIBLE_MINIO_USE_SSL:-false}" == "true" ]] && proto="https"
        local ssl_flag="true"
        [[ "${CRUCIBLE_MINIO_USE_SSL:-false}" == "true" ]] && ssl_flag="false"
        export AWS_ACCESS_KEY_ID="${CRUCIBLE_MINIO_ACCESS_KEY:?CRUCIBLE_MINIO_ACCESS_KEY is required for the built-in Pulumi backend}"
        export AWS_SECRET_ACCESS_KEY="${CRUCIBLE_MINIO_SECRET_KEY:?CRUCIBLE_MINIO_SECRET_KEY is required for the built-in Pulumi backend}"
        export AWS_REGION=us-east-1
        export PULUMI_BACKEND_URL="s3://${CRUCIBLE_MINIO_BUCKET_STATE}?region=us-east-1&endpoint=${proto}://${CRUCIBLE_MINIO_ENDPOINT}&disableSSL=${ssl_flag}&s3ForcePathStyle=true"
        log "backend: minio (${proto}://${CRUCIBLE_MINIO_ENDPOINT}/${CRUCIBLE_MINIO_BUCKET_STATE})"
    else
        log "backend: ${PULUMI_BACKEND_URL} (from PULUMI_BACKEND_URL stack env var)"
    fi

    # Install language plugins and program dependencies (npm install, pip install, etc.)
    log "installing dependencies"
    pulumi install --non-interactive 2>&1 \
        || log "warn: pulumi install had warnings — continuing"

    # Select the stack; create it on first run.
    pulumi stack select "${stack}" --non-interactive 2>/dev/null \
        || pulumi stack init "${stack}" --non-interactive
    log "stack: ${stack}"

    case "${CRUCIBLE_RUN_TYPE}" in
        apply)
            # Second phase: confirmed run re-enqueues with run_type=apply.
            log "applying: ${stack}"
            report_status "applying"
            # Download the saved preview text for audit (best-effort).
            curl -sf "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan" \
                -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
                -o /workspace/pulumi_preview.txt \
                || log "warn: could not retrieve preview artifact (non-fatal)"
            pulumi up --yes --non-interactive --stack "${stack}" 2>&1
            ;;

        destroy)
            log "running destroy preview: ${stack}"
            report_status "planning"
            pulumi preview --destroy --diff --non-interactive --stack "${stack}" \
                2>&1 | tee /workspace/pulumi_preview.txt || {
                upload_plan /workspace/pulumi_preview.txt
                fail "pulumi destroy preview failed"
            }
            upload_plan /workspace/pulumi_preview.txt
            pulumi preview --destroy --json --non-interactive --stack "${stack}" \
                > /workspace/pulumi_preview.json 2>/dev/null || true
            report_pulumi_summary /workspace/pulumi_preview.json
            log "destroy preview complete — awaiting confirmation"
            ;;

        proposed|tracked|*)
            # Plan phase: run preview, capture text diff + JSON summary.
            log "running preview: ${stack}"
            report_status "planning"
            pulumi preview --diff --non-interactive --stack "${stack}" \
                2>&1 | tee /workspace/pulumi_preview.txt || {
                upload_plan /workspace/pulumi_preview.txt
                fail "pulumi preview failed"
            }
            upload_plan /workspace/pulumi_preview.txt
            # Second preview pass for machine-readable summary counts.
            pulumi preview --json --non-interactive --stack "${stack}" \
                > /workspace/pulumi_preview.json 2>/dev/null || true
            report_pulumi_summary /workspace/pulumi_preview.json
            if [[ "${CRUCIBLE_RUN_TYPE}" == "proposed" ]]; then
                log "preview complete (proposed — no apply)"
            else
                log "preview complete — awaiting confirmation"
            fi
            ;;
    esac
}

# ── VCS authentication ────────────────────────────────────────────────────────
# If a VCS token is provided, write a .netrc so git picks it up automatically.
# Supports GitHub, GitLab, Gitea and any host that accepts token-based HTTP auth.
if [[ -n "${CRUCIBLE_VCS_TOKEN}" ]]; then
    log "VCS auth: configuring token authentication"
    # Extract host from repo URL (handles https://host/... and git@host:...)
    REPO_HOST=$(echo "${CRUCIBLE_REPO_URL}" | sed -E 's|https?://([^/]+)/.*|\1|; s|git@([^:]+):.*|\1|')
    log "VCS auth: writing .netrc for host ${REPO_HOST}"
    cat > /workspace/.netrc <<EOF
machine ${REPO_HOST}
login x-token
password ${CRUCIBLE_VCS_TOKEN}
EOF
    chmod 600 /workspace/.netrc
    export HOME=/workspace
    export GIT_CONFIG_NOSYSTEM=1
else
    log "VCS auth: none (public repo or no integration assigned)"
fi

# ── Clone repository ──────────────────────────────────────────────────────────
log "cloning ${CRUCIBLE_REPO_URL} @ ${CRUCIBLE_REPO_BRANCH}"
git clone --depth=1 --branch "${CRUCIBLE_REPO_BRANCH}" \
    "${CRUCIBLE_REPO_URL}" /workspace/repo 2>&1 \
    || fail "git clone failed — check repo URL, branch name, and VCS integration token"

WORKDIR="/workspace/repo/${CRUCIBLE_PROJECT_ROOT}"
[[ -d "${WORKDIR}" ]] || fail "project root '${CRUCIBLE_PROJECT_ROOT}' not found in repository (check stack project root setting)"
log "working directory: ${WORKDIR}"
cd "${WORKDIR}"

# ── Dispatch ──────────────────────────────────────────────────────────────────
case "${CRUCIBLE_TOOL}" in
    opentofu)   run_opentofu  ;;
    terraform)  run_terraform ;;
    ansible)    run_ansible   ;;
    pulumi)     run_pulumi    ;;
    *)          fail "unsupported tool: ${CRUCIBLE_TOOL}" ;;
esac

log "done"
