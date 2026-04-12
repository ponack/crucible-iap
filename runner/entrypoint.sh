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
