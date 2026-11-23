# Integration tests

Integration tests that compile, plan, apply, verify, update, and destroy
real unobin stacks using the `unobin-library-aws` module. Both `emulator` and
`live` tiers run the same scenarios:

- `emulator`: Runs against Floci by default. The CloudWatch Logs, RDS, and
  Scheduler scenarios select MiniStack with a `.backend` file. Both
  emulators must be running when invoking the driver directly.
- `live`: Runs against real AWS. `UNOBIN_AWS_LIVE=1` must be set so it
  does not incur costs by accident.

The tier only changes which environment variables `run.sh` exports before
invoking the scenarios. For the emulator tier, dummy credentials, region, and
a per-scenario endpoint URL are defined with environment variables. For live
tests, the environment must already contain the credentials and region. Each
scenario factory gives `aws-config` a default empty object so the SDK's config
loader reads everything from the environment.

Layout:

```
scenarios/
  <scenario>/
    factory.ub        # unobin factory -- imports aws and binds `aws-config`
    config.ub         # operator config; env supplies AWS settings by default
    config-update.ub  # same config with changed inputs for the update pass
    .backend          # optional; set to ministack to select MiniStack
    verify/main.go    # `go run`-able Go program that reads AWS state and
                      #   asserts it matches the phase in VERIFY_PHASE
```

## Run

```sh
# Start both emulators and run the test driver in a container.
make test-integration-emulator

# Run the driver directly with Floci on http://localhost:4566 and
# MiniStack on http://localhost:4567 (or FLOCI_ENDPOINT / MINISTACK_ENDPOINT).
export UNOBIN_VERSION=$(awk '/github.com[/]cloudboss[/]unobin v/{print $2}' go.mod)
export GOPATH=$(go env GOPATH)
./tests/integration/run.sh emulator

# Live: needs real AWS credentials and region in env.
UNOBIN_AWS_LIVE=1 ./tests/integration/run.sh live

# Single scenario:
make test-integration-emulator SCENARIO=ec2-vpc
```

## What the driver does per scenario

1. `unobin compile -p` the scenario directory and build the factory binary.
2. `./<scenario> pin` both configs. The stack name is the config file's
   basename and the state is scoped by stack, so the update config is staged
   as `update/config.ub`: both passes then address the same stack and state.
3. `./<scenario> plan -c config.ub -o plan.json`, then
   `./<scenario> apply plan.json`.
4. `VERIFY_PHASE=applied go run ./<scenario>/verify` -- assert the resources
   are present.
5. `./<scenario> plan -c update/config.ub -o plan-update.json`, then
   `./<scenario> apply plan-update.json` -- exercise the in-place update paths
   with the changed inputs.
6. `./<scenario> plan --destroy -c <config> -o destroy.json`, then
   `./<scenario> apply destroy.json` to tear everything down. The destroy plans
   from the config of the most recent apply attempt, so a run that failed
   before the update pass still tears down with the inputs that built it.
   Destroy runs even after an earlier failure; if the destroy itself also
   fails, the run reports both, since resources are left behind.
7. `VERIFY_PHASE=destroyed go run ./<scenario>/verify` -- assert nothing is
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

- Go toolchain.
- Floci (default port 4566) and MiniStack (default port 4567) for the emulator
  tier. `make emulators-up` starts both.
