#!/bin/sh
# Integration test driver.
#
# Usage: run.sh <tier>
#   tier:  emulator | live
#
# Environment:
#   SCENARIO=<name>      Run only one scenario directory.
#   UNOBIN_VERSION       Version of unobin to use for the test.
#   LOCALSTACK_ENDPOINT  Endpoint of the LocalStack emulator, defaults to
#                        http://localhost:4566.
#   MINISTACK_ENDPOINT   Endpoint of the ministack emulator, defaults to
#                        http://localhost:4567.
#
# The emulator tier runs each scenario against ministack unless the scenario
# directory holds a .backend file naming localstack, which pins a scenario
# whose operations ministack does not support. The driver exports dummy AWS
# credentials, AWS_REGION, and the chosen emulator's endpoint as
# AWS_ENDPOINT_URL before invoking each scenario. For the live tier, the
# environment must already contain real credentials and region. A scenario with
# a .skip-<tier> file is skipped on that tier and runs on the others; the file
# holds the reason, which is printed when the scenario is skipped.

set -eu

# HTTP probe via curl or wget.
healthcheck() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsS "${1}" >/dev/null 2>&1
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O /dev/null "${1}"
    else
        return 0
    fi
}

if [ ${#} -lt 1 ]; then
    echo "usage: ${0} <emulator|live>" >&2
    exit 2
fi

TIER="${1}"
case "${TIER}" in
    emulator|live) ;;
    *)
        echo "unknown tier ${TIER}; expected emulator or live" >&2
        exit 2
        ;;
esac

if [ "${TIER}" = "live" ] && [ "${UNOBIN_AWS_LIVE:-0}" != "1" ]; then
    echo "live scenarios require UNOBIN_AWS_LIVE=1" >&2
    exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${0}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
UNOBIN_VERSION="${UNOBIN_VERSION:?UNOBIN_VERSION is required}"
SCENARIOS_DIR="${SCRIPT_DIR}/scenarios"

if [ "${TIER}" = "emulator" ]; then
    LOCALSTACK_ENDPOINT="${LOCALSTACK_ENDPOINT:-http://localhost:4566}"
    MINISTACK_ENDPOINT="${MINISTACK_ENDPOINT:-http://localhost:4567}"
    for endpoint in "${LOCALSTACK_ENDPOINT}" "${MINISTACK_ENDPOINT}"; do
        if ! healthcheck "${endpoint}/_localstack/health"; then
            echo "emulator is not reachable at ${endpoint}" >&2
            exit 2
        fi
    done
    export AWS_ACCESS_KEY_ID=test
    export AWS_SECRET_ACCESS_KEY=test
    export AWS_REGION=us-east-1
fi

# Populate the scenario list via positional parameters. Iterate every directory
# under scenarios/ that contains a factory.ub unless SCENARIO is defined.
SELECT="${SCENARIO:-}"
if [ -n "${SELECT}" ]; then
    if [ ! -d "${SCENARIOS_DIR}/${SELECT}" ]; then
        echo "SCENARIO=${SELECT} not found under ${SCENARIOS_DIR}" >&2
        exit 2
    fi
    set -- "${SCENARIOS_DIR}/${SELECT}"
else
    set --
    for d in "${SCENARIOS_DIR}"/*/; do
        [ -f "${d}/factory.ub" ] || continue
        set -- "${@}" "${d%/}"
    done
fi

if [ ${#} -eq 0 ]; then
    echo "no scenarios under ${SCENARIOS_DIR}" >&2
    exit 2
fi

go install github.com/cloudboss/unobin/cmd/unobin@${UNOBIN_VERSION}
UNOBIN=${GOPATH}/bin/unobin

# The work directory lives under _output so it survives the test
# container, which mounts the repo and removes itself on exit. A clean
# run removes the directory; a failed or interrupted one keeps it, since
# each scenario's .unobin/state inside is the only record of what a
# manual teardown still has to remove.
mkdir -p "${REPO_DIR}/_output/integration"
tmp_dir=$(mktemp -d "${REPO_DIR}/_output/integration/run-XXXXXX")
cleanup() {
    if [ -z "${FAILED}" ] && [ -n "${RUN_COMPLETE}" ]; then
        rm -rf "${tmp_dir}"
    else
        echo "keeping ${tmp_dir} with the state of the incomplete run" >&2
    fi
}
trap cleanup EXIT

FAILED=""
RUN_COMPLETE=""
COUNT=0
for sdir in "${@}"; do
    COUNT=$((COUNT + 1))
    name=$(basename "${sdir}")
    # A scenario opts out of a tier with a .skip-<tier> file holding a reason
    # (printed below); the scenario still runs on every other tier.
    if [ -f "${sdir}/.skip-${TIER}" ]; then
        echo "==> skip ${TIER}/${name} ($(cat "${sdir}/.skip-${TIER}"))"
        continue
    fi
    # Each scenario picks its emulator: ministack unless a .backend file pins
    # it to localstack. The endpoint is exported per scenario so one run
    # serves scenarios on both emulators.
    if [ "${TIER}" = "emulator" ]; then
        backend="ministack"
        if [ -f "${sdir}/.backend" ]; then
            backend=$(cat "${sdir}/.backend")
        fi
        case "${backend}" in
            ministack) export AWS_ENDPOINT_URL="${MINISTACK_ENDPOINT}" ;;
            localstack) export AWS_ENDPOINT_URL="${LOCALSTACK_ENDPOINT}" ;;
            *)
                echo "unknown backend ${backend} in ${sdir}/.backend" >&2
                FAILED="${FAILED} ${name}(backend)"
                continue
                ;;
        esac
        echo "==> ${TIER}/${name} (${backend})"
    else
        echo "==> ${TIER}/${name}"
    fi
    missing=""
    for f in factory.ub config.ub config-update.ub; do
        if [ ! -f "${sdir}/${f}" ]; then
            echo "missing ${f}" >&2
            missing="true"
        fi
    done
    if [ -n "${missing}" ]; then
        FAILED="${FAILED} ${name}"
        continue
    fi

    build_dir="${tmp_dir}/${name}"
    rel="${sdir#${REPO_DIR}/}"

    # failed_step holds the first failure seen and stays the reported reason no
    # matter what fails after it. Once apply is attempted, the scenario may have
    # created cloud resources, so destroy runs even after an earlier failure to
    # tear them down; a destroy failure is reported only when nothing failed
    # before it. destroy_config names the config of the most recent apply
    # attempt, so destroy plans from the same inputs that produced the state.
    failed_step=""
    applied=""
    destroy_config="config.ub"

    if [ -d "${sdir}/prepare" ]; then
        (
            cd "${REPO_DIR}"
            SCENARIO_DIR="${sdir}" go run "./${rel}/prepare"
        ) || failed_step="prepare"
    fi

    if [ -z "${failed_step}" ]; then
        (
            ${UNOBIN} compile \
                -p "${sdir}" \
                -o "${build_dir}" \
                --build
        ) || failed_step="compile"
    fi

    # The stack name is the config file's basename, and the state is scoped
    # by stack, so both passes must plan under the same basename or the
    # update would address an empty state and recreate everything. The
    # update config is staged as update/config.ub to keep the name.
    if [ -z "${failed_step}" ]; then
        (
            cd "${build_dir}"
            cp "${sdir}/config.ub" .
            mkdir -p update
            cp "${sdir}/config-update.ub" update/config.ub
            ./${name} pin -c config.ub
            ./${name} pin -c update/config.ub
        ) || failed_step="pin"
    fi

    if [ -z "${failed_step}" ]; then
        (
            [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
            cd "${build_dir}"
            ./${name} plan \
                -c ./config.ub \
                -o "${build_dir}/plan.json"
        ) || failed_step="plan"
    fi

    if [ -z "${failed_step}" ]; then
        # apply is the step that creates cloud resources, so from here a failed
        # run has something to tear down even when apply itself fails partway.
        applied="true"
        (
            cd "${build_dir}"
            ./${name} apply "${build_dir}/plan.json"
        ) || failed_step="apply"
    fi

    if [ -z "${failed_step}" ] && [ -d "${sdir}/verify" ]; then
        (
            [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
            cd "${REPO_DIR}"
            VERIFY_BUILD_DIR="${build_dir}" VERIFY_PHASE=applied go run "./${rel}/verify"
        ) || failed_step="verify-applied"
    fi

    # The update pass replans from the staged update config and applies the
    # difference, exercising each resource's in-place update path.
    if [ -z "${failed_step}" ]; then
        (
            [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
            cd "${build_dir}"
            ./${name} plan \
                -c ./update/config.ub \
                -o "${build_dir}/plan-update.json"
        ) || failed_step="plan-update"
    fi

    if [ -z "${failed_step}" ]; then
        destroy_config="update/config.ub"
        (
            cd "${build_dir}"
            ./${name} apply "${build_dir}/plan-update.json"
        ) || failed_step="apply-update"
    fi

    if [ -z "${failed_step}" ] && [ -d "${sdir}/verify" ] && [ -f "${sdir}/.verify-updated" ]; then
        (
            [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
            cd "${REPO_DIR}"
            VERIFY_BUILD_DIR="${build_dir}" VERIFY_PHASE=updated go run "./${rel}/verify"
        ) || failed_step="verify-updated"
    fi

    # Destroy runs whenever apply was attempted, even after an earlier failure,
    # so a failed run still cleans up what it created. verify-destroyed runs only
    # on an otherwise-clean run, since a run that already failed keeps its first
    # error as the reason; a destroy failure after an earlier failure is appended
    # to it, since it means resources are left behind for manual cleanup.
    if [ -n "${applied}" ]; then
        if (
            [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
            cd "${build_dir}"
            ./${name} plan --destroy \
                -c "./${destroy_config}" \
                -o "${build_dir}/destroy.json"
            ./${name} apply "${build_dir}/destroy.json"
        ); then
            if [ -z "${failed_step}" ] && [ -d "${sdir}/verify" ]; then
                (
                    [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
                    cd "${REPO_DIR}"
                    VERIFY_BUILD_DIR="${build_dir}" VERIFY_PHASE=destroyed go run "./${rel}/verify"
                ) || failed_step="verify-destroyed"
            fi
        elif [ -z "${failed_step}" ]; then
            failed_step="destroy"
        else
            failed_step="${failed_step}+destroy"
        fi
    fi

    if [ -n "${failed_step}" ]; then
        FAILED="${FAILED} ${name}(${failed_step})"
    fi
done

RUN_COMPLETE="true"
if [ -n "${FAILED}" ]; then
    echo "FAIL:${FAILED}" >&2
    exit 1
fi
echo "OK: ${COUNT} scenario(s)"
