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

    log "initialising"
    ${bin} init -no-color

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
run_ansible() {
    local playbook="${CRUCIBLE_ANSIBLE_PLAYBOOK:-site.yml}"
    log "running ansible-playbook ${playbook}"
    report_status "planning"
    ansible-playbook "${playbook}" --diff
}

# ── Pulumi ────────────────────────────────────────────────────────────────────
run_pulumi() {
    fail "pulumi runner not yet implemented"
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
