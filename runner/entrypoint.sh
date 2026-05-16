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

upload_plan_json() {
    local bin="$1"
    local plan_file="$2"
    [[ -f "${plan_file}" ]] || return 0
    log "generating plan JSON"
    if ! "${bin}" show -json "${plan_file}" > /workspace/plan.json 2>/dev/null; then
        log "warn: plan JSON generation failed — skipping"
        return 0
    fi
    log "uploading plan JSON for diff"
    curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan-json" \
        -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
        -H "Content-Type: application/json" \
        --data-binary "@/workspace/plan.json" \
        || log "warn: plan JSON upload failed"
}

# ── Infracost ─────────────────────────────────────────────────────────────────
# Runs after upload_plan_json has saved /workspace/plan.json.
# Reports monthly cost add/remove to the Crucible cost API.
# Silently skipped when INFRACOST_API_KEY is unset or infracost is not installed.
report_infracost() {
    [[ -n "${INFRACOST_API_KEY:-}" ]] || return 0
    [[ -f /workspace/plan.json ]] || { log "info: no plan JSON — skipping Infracost"; return 0; }
    command -v infracost &>/dev/null || { log "warn: infracost binary not found — skipping cost estimate"; return 0; }

    log "running infracost breakdown"
    local cost_json
    if ! cost_json=$(infracost breakdown --path /workspace/plan.json --format json 2>/tmp/infracost.err); then
        log "warn: infracost breakdown failed — $(head -3 /tmp/infracost.err 2>/dev/null)"
        return 0
    fi

    local currency diff cost_add cost_remove
    currency=$(printf '%s' "${cost_json}" | jq -r '.currency // "USD"')
    diff=$(printf '%s' "${cost_json}"     | jq -r '.diffTotalMonthlyCost // "0"')
    cost_add=$(printf '%s'    "${diff}" | awk '{v=$1+0; printf "%.4f", (v>0)?v:0}')
    cost_remove=$(printf '%s' "${diff}" | awk '{v=$1+0; printf "%.4f", (v<0)?-v:0}')

    log "infracost: add=${cost_add} remove=${cost_remove} currency=${currency}"
    curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/cost" \
        "${API_HEADERS[@]}" \
        -d "{\"monthly_cost_add\":${cost_add},\"monthly_cost_change\":0,\"monthly_cost_remove\":${cost_remove},\"currency\":\"${currency}\"}" \
        || log "warn: failed to report infracost data"
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
        [[ -z "$key" ]] && continue
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
            local http_code
            http_code=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
                -H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" \
                -H "Content-Type: application/octet-stream" \
                --data-binary "@${file}" \
                "${CRUCIBLE_API_URL}/api/v1/internal/provider-cache/${key}")
            if [[ "${http_code}" == "204" ]]; then
                count=$(( count + 1 ))
            else
                log "warn: failed to cache provider ${key} (HTTP ${http_code})"
            fi
        fi
    done < <(find "${PROVIDER_CACHE_DIR}" -type f)

    log "provider cache: uploaded ${count} new provider(s)"
}

# ── Lifecycle hooks ───────────────────────────────────────────────────────────
# Executes an optional hook script injected as an env var by the dispatcher.
# The script runs in a bash subshell so it inherits all exported env vars but
# cannot modify the runner's shell state.
run_hook() {
    local name="$1"
    local script="${2:-}"
    [[ -n "${script}" ]] || return 0
    log "running ${name} hook"
    bash -c "${script}" || fail "${name} hook exited with error"
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

    # Inject an override file that forces the HTTP backend.
    # TF_HTTP_* env vars are only read when the backend type is "http" — if the
    # user's code omits a backend block entirely (or uses a different backend),
    # Terraform silently writes state to the ephemeral tmpfs and it is lost.
    # Writing to crucible_backend_override.tf.json (cwd = WORKDIR at call time)
    # ensures the HTTP backend is always selected. TF_HTTP_* env vars then supply
    # all the actual settings so an empty block is sufficient.
    cat > crucible_backend_override.tf.json <<'TFEOF'
{
  "terraform": [{
    "backend": [{"http": [{}]}]
  }]
}
TFEOF

    export TF_PLUGIN_CACHE_DIR="${PROVIDER_CACHE_DIR}"
    restore_provider_cache

    log "initialising"
    ${bin} init -no-color -reconfigure

    save_provider_cache

    case "${CRUCIBLE_RUN_TYPE}" in
        destroy)
            log "planning destroy"
            report_status "planning"
            run_hook "pre_plan" "${CRUCIBLE_HOOK_PRE_PLAN:-}"
            ${bin} plan -no-color -destroy -out=/workspace/plan.tfplan
            upload_plan /workspace/plan.tfplan
            upload_plan_json "${bin}" /workspace/plan.tfplan
            report_infracost
            run_hook "post_plan" "${CRUCIBLE_HOOK_POST_PLAN:-}"
            log "plan complete — awaiting confirmation before destroy"
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
            run_hook "pre_apply" "${CRUCIBLE_HOOK_PRE_APPLY:-}"
            ${bin} apply -no-color /workspace/plan.tfplan
            run_hook "post_apply" "${CRUCIBLE_HOOK_POST_APPLY:-}"
            ;;

        proposed)
            # Plan only — no apply, no confirmation needed.
            log "running plan (proposed)"
            report_status "planning"
            run_hook "pre_plan" "${CRUCIBLE_HOOK_PRE_PLAN:-}"
            ${bin} plan -no-color -out=/workspace/plan.tfplan
            upload_plan /workspace/plan.tfplan
            upload_plan_json "${bin}" /workspace/plan.tfplan
            report_infracost
            run_hook "post_plan" "${CRUCIBLE_HOOK_POST_PLAN:-}"
            ;;

        tracked|*)
            # Default: plan, upload, then wait for human confirmation.
            log "running plan"
            report_status "planning"
            run_hook "pre_plan" "${CRUCIBLE_HOOK_PRE_PLAN:-}"
            ${bin} plan -no-color -out=/workspace/plan.tfplan
            upload_plan /workspace/plan.tfplan
            upload_plan_json "${bin}" /workspace/plan.tfplan
            report_infracost
            run_hook "post_plan" "${CRUCIBLE_HOOK_POST_PLAN:-}"
            log "plan complete — awaiting confirmation"
            ;;
    esac
}

# Download a pinned OpenTofu version and return the binary path via stdout.
# Prints the system "tofu" path when CRUCIBLE_TOOL_VERSION is unset or empty.
resolve_opentofu_bin() {
    local ver="${CRUCIBLE_TOOL_VERSION:-}"
    if [[ -z "${ver}" ]]; then
        echo "tofu"
        return
    fi
    local arch
    arch="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
    local dest="/tmp/versioned/tofu-${ver}"
    if [[ ! -x "${dest}" ]]; then
        log "downloading opentofu v${ver} (${arch})"
        mkdir -p /tmp/versioned
        curl -fsSL \
            "https://github.com/opentofu/opentofu/releases/download/v${ver}/tofu_${ver}_linux_${arch}.zip" \
            -o /tmp/tofu-download.zip \
            || fail "failed to download opentofu v${ver}"
        unzip -q /tmp/tofu-download.zip tofu -d /tmp/versioned-extract/ \
            || fail "failed to extract opentofu v${ver}"
        mv /tmp/versioned-extract/tofu "${dest}"
        chmod +x "${dest}"
        rm -f /tmp/tofu-download.zip
    fi
    echo "${dest}"
}

# Download a pinned Terraform version and return the binary path via stdout.
# Prints the system "terraform" path when CRUCIBLE_TOOL_VERSION is unset or empty.
resolve_terraform_bin() {
    local ver="${CRUCIBLE_TOOL_VERSION:-}"
    if [[ -z "${ver}" ]]; then
        echo "terraform"
        return
    fi
    local arch
    arch="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
    local dest="/tmp/versioned/terraform-${ver}"
    if [[ ! -x "${dest}" ]]; then
        log "downloading terraform v${ver} (${arch})"
        mkdir -p /tmp/versioned
        curl -fsSL \
            "https://releases.hashicorp.com/terraform/${ver}/terraform_${ver}_linux_${arch}.zip" \
            -o /tmp/terraform-download.zip \
            || fail "failed to download terraform v${ver}"
        unzip -q /tmp/terraform-download.zip terraform -d /tmp/versioned-extract/ \
            || fail "failed to extract terraform v${ver}"
        mv /tmp/versioned-extract/terraform "${dest}"
        chmod +x "${dest}"
        rm -f /tmp/terraform-download.zip
    fi
    echo "${dest}"
}

run_opentofu()  { run_tf_generic "$(resolve_opentofu_bin)"; }
run_terraform() { run_tf_generic "$(resolve_terraform_bin)"; }

# ── Terragrunt ────────────────────────────────────────────────────────────────

resolve_terragrunt_bin() {
    local ver="${CRUCIBLE_TOOL_VERSION:-0.72.1}"
    local arch
    arch="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
    local dest="/tmp/versioned/terragrunt-${ver}"
    if [[ ! -x "${dest}" ]]; then
        log "downloading terragrunt v${ver} (${arch})"
        mkdir -p /tmp/versioned
        curl -fsSL \
            "https://github.com/gruntwork-io/terragrunt/releases/download/v${ver}/terragrunt_linux_${arch}" \
            -o "${dest}" \
            || fail "failed to download terragrunt v${ver}"
        chmod +x "${dest}"
    fi
    echo "${dest}"
}

run_terragrunt() {
    local tg
    tg="$(resolve_terragrunt_bin)"
    log "tool=terragrunt run_type=${CRUCIBLE_RUN_TYPE}"
    log "version: $(${tg} --version 2>&1 | head -1)"

    # Terragrunt manages its own state backend via remote_state blocks.
    # Point the underlying OpenTofu / Terraform HTTP backend at Crucible so that
    # stacks using the built-in state backend work without extra config.
    export TF_HTTP_ADDRESS="${CRUCIBLE_API_URL}/api/v1/state/${CRUCIBLE_STACK_ID}"
    export TF_HTTP_LOCK_ADDRESS="${TF_HTTP_ADDRESS}"
    export TF_HTTP_UNLOCK_ADDRESS="${TF_HTTP_ADDRESS}"
    export TF_HTTP_USERNAME="${CRUCIBLE_STACK_ID}"
    export TF_HTTP_PASSWORD="${CRUCIBLE_JOB_TOKEN}"
    export TF_IN_AUTOMATION=1
    export TF_INPUT=0

    case "${CRUCIBLE_RUN_TYPE}" in
        destroy)
            log "running terragrunt run-all destroy"
            report_status "applying"
            run_hook "pre_apply" "${CRUCIBLE_HOOK_PRE_APPLY:-}"
            ${tg} run-all destroy --terragrunt-non-interactive --auto-approve
            run_hook "post_apply" "${CRUCIBLE_HOOK_POST_APPLY:-}"
            ;;

        apply)
            log "running terragrunt run-all apply"
            report_status "applying"
            run_hook "pre_apply" "${CRUCIBLE_HOOK_PRE_APPLY:-}"
            ${tg} run-all apply --terragrunt-non-interactive --auto-approve
            run_hook "post_apply" "${CRUCIBLE_HOOK_POST_APPLY:-}"
            ;;

        proposed)
            log "running terragrunt run-all plan (proposed)"
            report_status "planning"
            run_hook "pre_plan" "${CRUCIBLE_HOOK_PRE_PLAN:-}"
            ${tg} run-all plan --terragrunt-non-interactive
            run_hook "post_plan" "${CRUCIBLE_HOOK_POST_PLAN:-}"
            ;;

        tracked|*)
            log "running terragrunt run-all plan"
            report_status "planning"
            run_hook "pre_plan" "${CRUCIBLE_HOOK_PRE_PLAN:-}"
            ${tg} run-all plan --terragrunt-non-interactive
            run_hook "post_plan" "${CRUCIBLE_HOOK_POST_PLAN:-}"
            log "plan complete — awaiting confirmation"
            ;;
    esac
}

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

# ── OIDC token files ─────────────────────────────────────────────────────────
# Written here (inside the running container) rather than injected via
# CopyToContainer before start — tmpfs /tmp is only writable once running.
if [[ -n "${CRUCIBLE_OIDC_TOKEN:-}" ]]; then
    printf '%s' "${CRUCIBLE_OIDC_TOKEN}" > /tmp/oidc-token
    log "oidc: token written to /tmp/oidc-token"
fi
if [[ -n "${CRUCIBLE_OIDC_GCP_CREDENTIALS:-}" ]]; then
    printf '%s' "${CRUCIBLE_OIDC_GCP_CREDENTIALS}" > /tmp/gcp-credentials.json
    log "oidc: GCP credentials written to /tmp/gcp-credentials.json"
fi

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
    opentofu)    run_opentofu   ;;
    terraform)   run_terraform  ;;
    ansible)     run_ansible    ;;
    pulumi)      run_pulumi     ;;
    terragrunt)  run_terragrunt ;;
    *)           fail "unsupported tool: ${CRUCIBLE_TOOL}" ;;
esac

log "done"
