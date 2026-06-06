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
# environment must already contain real credentials and region.

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
# under scenarios/ that contains a main.ub unless SCENARIO is defined.
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
        [ -d "${d}" ] || continue
        set -- "${@}" "${d%/}"
    done
fi

if [ ${#} -eq 0 ]; then
    echo "no scenarios under ${SCENARIOS_DIR}" >&2
    exit 2
fi

go install github.com/cloudboss/unobin/cmd/unobin@${UNOBIN_VERSION}
UNOBIN=${GOPATH}/bin/unobin

tmp_dir=$(mktemp -d "/tmp/unobin-library-aws-XXXXXX")
trap "rm -rf ${tmp_dir}" EXIT

FAILED=""
COUNT=0
for sdir in "${@}"; do
    COUNT=$((COUNT + 1))
    name=$(basename "${sdir}")
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
    if [ ! -f "${sdir}/main.ub" ]; then
        echo "missing main.ub" >&2
        FAILED="${FAILED} ${name}"
        continue
    fi

    build_dir="${tmp_dir}/${name}"
    rel="${sdir#${REPO_DIR}/}"

    # failed_step holds the first failure seen and stays the reported reason no
    # matter what fails after it. Once apply is attempted, the scenario may have
    # created cloud resources, so destroy runs even after an earlier failure to
    # tear them down; a destroy failure is reported only when nothing failed
    # before it.
    failed_step=""
    applied=""

    if [ -d "${sdir}/prepare" ]; then
        (
            cd "${REPO_DIR}"
            SCENARIO_DIR="${sdir}" go run "./${rel}/prepare"
        ) || failed_step="prepare"
    fi

    if [ -z "${failed_step}" ]; then
        (
            ${UNOBIN} compile \
                -p "${sdir}/main.ub" \
                -o "${build_dir}" \
                --build
        ) || failed_step="compile"
    fi

    if [ -z "${failed_step}" ]; then
        (
            cd "${build_dir}"
            cp "${sdir}/config.ub" .
            ./${name} pin -c config.ub
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
            VERIFY_PHASE=applied go run "./${rel}/verify"
        ) || failed_step="verify-applied"
    fi

    # Destroy runs whenever apply was attempted, even after an earlier failure,
    # so a failed run still cleans up what it created. verify-destroyed runs only
    # on an otherwise-clean run, since a run that already failed keeps its first
    # error as the reason.
    if [ -n "${applied}" ]; then
        if (
            [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
            cd "${build_dir}"
            ./${name} plan --destroy \
                -c ./config.ub \
                -o "${build_dir}/destroy.json"
            ./${name} apply "${build_dir}/destroy.json"
        ); then
            if [ -z "${failed_step}" ] && [ -d "${sdir}/verify" ]; then
                (
                    [ -f "${sdir}/.env-${TIER}" ] && set -a && . "${sdir}/.env-${TIER}"
                    cd "${REPO_DIR}"
                    VERIFY_PHASE=destroyed go run "./${rel}/verify"
                ) || failed_step="verify-destroyed"
            fi
        elif [ -z "${failed_step}" ]; then
            failed_step="destroy"
        fi
    fi

    if [ -n "${failed_step}" ]; then
        FAILED="${FAILED} ${name}(${failed_step})"
    fi
done

if [ -n "${FAILED}" ]; then
    echo "FAIL:${FAILED}" >&2
    exit 1
fi
echo "OK: ${COUNT} scenario(s)"
