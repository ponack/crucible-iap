#!/usr/bin/env bash
# Crucible IAP — Runner Entrypoint
# Executed inside the ephemeral job container.
# All env vars are injected by the dispatcher at container spawn time.
set -euo pipefail

log() { echo "[crucible-runner] $*"; }
fail() { log "ERROR: $*"; exit 1; }

# ── Validate required environment ────────────────────────────────────────────
: "${CRUCIBLE_RUN_ID:?CRUCIBLE_RUN_ID is required}"
: "${CRUCIBLE_API_URL:?CRUCIBLE_API_URL is required}"
: "${CRUCIBLE_JOB_TOKEN:?CRUCIBLE_JOB_TOKEN is required}"
: "${CRUCIBLE_TOOL:?CRUCIBLE_TOOL is required}"
: "${CRUCIBLE_REPO_URL:?CRUCIBLE_REPO_URL is required}"
: "${CRUCIBLE_REPO_BRANCH:=${CRUCIBLE_REPO_BRANCH:-main}}"
: "${CRUCIBLE_PROJECT_ROOT:=${CRUCIBLE_PROJECT_ROOT:-.}}"
: "${CRUCIBLE_RUN_TYPE:=${CRUCIBLE_RUN_TYPE:-tracked}}"

API_HEADERS=(-H "Authorization: Bearer ${CRUCIBLE_JOB_TOKEN}" -H "Content-Type: application/json")

# ── Helper: report status back to API ────────────────────────────────────────
report_status() {
    local status="$1"
    curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/status" \
        "${API_HEADERS[@]}" \
        -d "{\"status\":\"${status}\"}" || log "warn: failed to report status ${status}"
}

# ── Clone repository ──────────────────────────────────────────────────────────
log "cloning ${CRUCIBLE_REPO_URL} @ ${CRUCIBLE_REPO_BRANCH}"
git clone --depth=1 --branch "${CRUCIBLE_REPO_BRANCH}" "${CRUCIBLE_REPO_URL}" /workspace/repo \
    || fail "git clone failed"

WORKDIR="/workspace/repo/${CRUCIBLE_PROJECT_ROOT}"
cd "${WORKDIR}"

# ── Dispatch to tool handler ──────────────────────────────────────────────────
case "${CRUCIBLE_TOOL}" in
    opentofu)   run_opentofu ;;
    terraform)  run_terraform ;;
    ansible)    run_ansible ;;
    pulumi)     run_pulumi ;;
    *)          fail "unsupported tool: ${CRUCIBLE_TOOL}" ;;
esac

# ── OpenTofu / Terraform ──────────────────────────────────────────────────────
run_opentofu() {
    local bin="tofu"
    run_tf_generic "${bin}"
}

run_terraform() {
    local bin="terraform"
    run_tf_generic "${bin}"
}

run_tf_generic() {
    local bin="$1"
    log "initialising with ${bin}"

    # Configure state backend to point at Crucible IAP
    export TF_HTTP_ADDRESS="${CRUCIBLE_API_URL}/api/v1/state/${CRUCIBLE_RUN_ID%%-*}"  # stack portion
    export TF_HTTP_LOCK_ADDRESS="${TF_HTTP_ADDRESS}"
    export TF_HTTP_UNLOCK_ADDRESS="${TF_HTTP_ADDRESS}"
    export TF_HTTP_USERNAME="${CRUCIBLE_RUN_ID}"
    export TF_HTTP_PASSWORD="${CRUCIBLE_JOB_TOKEN}"

    ${bin} init -input=false -no-color

    log "running plan"
    report_status "planning"
    ${bin} plan -input=false -no-color -out=/workspace/plan.tfplan

    # Upload plan artifact
    if [[ -f /workspace/plan.tfplan ]]; then
        curl -sf -X POST "${CRUCIBLE_API_URL}/api/v1/internal/runs/${CRUCIBLE_RUN_ID}/plan" \
            "${API_HEADERS[@]}" \
            --data-binary @/workspace/plan.tfplan || log "warn: plan upload failed"
    fi

    if [[ "${CRUCIBLE_RUN_TYPE}" == "apply" ]]; then
        log "applying"
        report_status "applying"
        ${bin} apply -input=false -no-color /workspace/plan.tfplan
    fi
}

# ── Ansible ───────────────────────────────────────────────────────────────────
run_ansible() {
    : "${CRUCIBLE_ANSIBLE_PLAYBOOK:=${CRUCIBLE_ANSIBLE_PLAYBOOK:-site.yml}}"
    log "running ansible-playbook ${CRUCIBLE_ANSIBLE_PLAYBOOK}"
    report_status "planning"
    ansible-playbook "${CRUCIBLE_ANSIBLE_PLAYBOOK}" --diff
}

# ── Pulumi ────────────────────────────────────────────────────────────────────
run_pulumi() {
    log "pulumi support coming soon"
    fail "pulumi runner not yet implemented"
}
