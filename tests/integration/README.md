# Integration tests

Integration tests that compile, plan, apply, verify, update, and destroy
real unobin stacks using the `unobin-library-aws` module. Both `emulator` and
`live` tiers run the same scenarios:

- `emulator`: Runs against AWS emulators in containers. Each scenario runs
  against ministack unless its directory holds a `.backend` file naming
  `localstack`, which pins it to [LocalStack](https://localstack.cloud).
  The two cover each other's gaps: LocalStack offers the ELB API only on
  its paid tier, so the load-balancing scenarios need ministack; ministack
  accepts `RevokeSecurityGroupEgress` without applying it and does not
  implement `ListOpenIDConnectProviders`, so `ec2-vpc` and `iam` pin to
  LocalStack. Both emulators must already be running before invoking.
- `live`: Runs against real AWS. `UNOBIN_AWS_LIVE=1` must be set so it
  does not incur costs by accident.

The tier only changes which environment variables `run.sh` exports before
invoking the scenarios. For the emulator tier, dummy credentials, region, and
a per-scenario endpoint URL are defined with environment variables. For live
tests, the environment must already contain the credentials and region. Each
scenario's `config.ub` and `config-update.ub` leave the AWS configuration
empty so the SDK's config loader reads everything from the environment.

Layout:

```
scenarios/
  <scenario>/
    main.ub           # unobin stack -- imports aws and uses one resource
    config.ub         # operator config; AWS block is `default: {}` so env wins
    config-update.ub  # same config with changed inputs for the update pass
    .backend          # optional; pins the scenario's emulator (default is
                      #   ministack)
    verify/main.go    # `go run`-able Go program that reads AWS state and
                      #   asserts it matches the phase in VERIFY_PHASE
```

## Run

```sh
# Local: needs LocalStack on http://localhost:4566 and ministack on
# http://localhost:4567 (or LOCALSTACK_ENDPOINT / MINISTACK_ENDPOINT).
# `make emulators-up` starts both; `make test-integration-emulator` does
# all of this in containers.
./tests/integration/run.sh emulator

# Live: needs real AWS credentials and region in env.
UNOBIN_AWS_LIVE=1 ./tests/integration/run.sh live

# Single scenario:
SCENARIO=ec2-vpc ./tests/integration/run.sh emulator
```

## What the driver does per scenario

1. `unobin compile` the stack with
   `--replace-go-module=github.com/cloudboss/unobin-library-aws=<repo>` so the
   compiled binary uses the in-tree code.
2. `go build` the compiled stack into `factory`.
3. `./factory pin` both configs. The stack name is the config file's
   basename and the state is scoped by stack, so the update config is staged
   as `update/config.ub`: both passes then address the same stack and state.
4. `./factory plan -c config.ub -o plan.json`, then `./factory apply plan.json`.
5. `VERIFY_PHASE=applied go run ./<scenario>/verify` -- assert the resources
   are present.
6. `./factory plan -c update/config.ub -o plan-update.json`, then
   `./factory apply plan-update.json` -- exercise the in-place update paths
   with the changed inputs.
7. `./factory plan --destroy -c <config> -o destroy.json`, then
   `./factory apply destroy.json` to tear everything down. The destroy plans
   from the config of the most recent apply attempt, so a run that failed
   before the update pass still tears down with the inputs that built it.
   Destroy runs even after an earlier failure; if the destroy itself also
   fails, the run reports both, since resources are left behind.
8. `VERIFY_PHASE=destroyed go run ./<scenario>/verify` -- assert nothing is
   left behind.

The driver builds and runs everything in a work directory under
`_output/integration/`. A clean run removes it; a failed or interrupted run
keeps it and prints the path, since each scenario's `.unobin/state` inside is
what a manual teardown works from.

The verifier only reads cloud state; it never creates or deletes. It loads
AWS config with `config.LoadDefaultConfig`, so the one program serves both
tiers, and it identifies resources by a stable attribute (the CIDR) since the
driver does not pass plan outputs in. The driver aborts on the first failure
and prints the failing step.

## Prerequisites

- `unobin` and `unobin-library-aws` checked out as siblings (the driver finds
  unobin via `../unobin` relative to this repo).
- Go toolchain.
- LocalStack (default port 4566) and ministack (default port 4567) for the
  emulator tier; `make emulators-up` starts both.
