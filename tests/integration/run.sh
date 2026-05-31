#!/bin/sh
# Integration test driver.
#
# Usage: run.sh <tier>
#   tier:  localstack | live
#
# Environment:
#   SCENARIO=<name>      Run only one scenario directory.
#   UNOBIN_VERSION       Version of unobin to use for the test.
#   LOCALSTACK_ENDPOINT  The localstack tier injects this as
#                        AWS_ENDPOINT_URL, defaults to http://localhost:4566.
#
# For the localstack tier, the driver exports dummy AWS credentials, AWS_REGION,
# and AWS_ENDPOINT_URL before invoking the scenarios. For the live tier, the
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
    echo "usage: ${0} <localstack|live>" >&2
    exit 2
fi

TIER="${1}"
case "${TIER}" in
    localstack|live) ;;
    *)
        echo "unknown tier ${TIER}; expected localstack or live" >&2
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

if [ "${TIER}" = "localstack" ]; then
    LOCALSTACK_ENDPOINT="${LOCALSTACK_ENDPOINT:-http://localhost:4566}"
    if ! healthcheck "${LOCALSTACK_ENDPOINT}/_localstack/health"; then
        echo "LocalStack is not reachable at ${LOCALSTACK_ENDPOINT}" >&2
        exit 2
    fi
    export AWS_ACCESS_KEY_ID=test
    export AWS_SECRET_ACCESS_KEY=test
    export AWS_REGION=us-east-1
    export AWS_ENDPOINT_URL="${LOCALSTACK_ENDPOINT}"
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

FAILED=""
COUNT=0
for sdir in "${@}"; do
    COUNT=$((COUNT + 1))
    name=$(basename "${sdir}")
    echo "==> ${TIER}/${name}"
    if [ ! -f "${sdir}/main.ub" ]; then
        echo "missing main.ub" >&2
        FAILED="${FAILED} ${name}"
        continue
    fi

    build_dir=$(mktemp -d "/tmp/unobin-library-aws-it-${name}-XXXXXX")
    rel="${sdir#${REPO_DIR}/}"

    failed_step=""
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
                --replace-go-module="github.com/cloudboss/unobin-library-aws=${REPO_DIR}" \
                --unobin-version="${UNOBIN_VERSION}" \
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
            cd "${build_dir}"
            ./${name} plan --allow-version-mismatch \
                -c ./config.ub \
                -o "${build_dir}/plan.json"
            ./${name} apply "${build_dir}/plan.json"
        ) || failed_step="apply"
    fi

    if [ -z "${failed_step}" ] && [ -d "${sdir}/verify" ]; then
        (
            cd "${REPO_DIR}"
            VERIFY_PHASE=applied go run "./${rel}/verify"
        ) || failed_step="verify-${VERIFY_PHASE}"
    fi

    if [ -z "${failed_step}" ]; then
        (
            cd "${build_dir}"
            ./${name} plan --allow-version-mismatch --destroy \
                -c ./config.ub \
                -o "${build_dir}/destroy.json"
            ./${name} apply "${build_dir}/destroy.json"
        ) || failed_step="destroy"
    fi

    if [ -z "${failed_step}" ] && [ -d "${sdir}/verify" ]; then
        (
            cd "${REPO_DIR}"
            VERIFY_PHASE=destroyed go run "./${rel}/verify"
        ) || failed_step="verify-${VERIFY_PHASE}"
    fi

    rm -rf "${build_dir}"

    if [ -n "${failed_step}" ]; then
        FAILED="${FAILED} ${name}(${failed_step})"
    fi
done

if [ -n "${FAILED}" ]; then
    echo "FAIL:${FAILED}" >&2
    exit 1
fi
echo "OK: ${COUNT} scenario(s)"
