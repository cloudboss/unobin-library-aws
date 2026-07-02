# lambdamicrovms-http

This example creates a Lambda MicroVM image from a small Python HTTP service,
runs a MicroVM, requests it through the authenticated MicroVM endpoint, captures
the response, and terminates the MicroVM.

## What it creates

- A private S3 bucket for the image source zip.
- A zip archive containing `app/Dockerfile` and `app/server.py`.
- An S3 object for that archive.
- A CloudWatch Logs log group for build and runtime logs.
- IAM build and execution roles with S3 and CloudWatch Logs permissions.
- A Lambda MicroVM image based on the first managed base image returned by AWS.
- A one-shot MicroVM run, auth token, health check, HTTP request, and terminate
  action.

## Before running

Edit `dev.ub`:

- Set `aws-config.region` and, if needed, `aws-config.profile`.
- Set `stack-key` to a lowercase value unique to your AWS account and region.

The example uses `curl` for the health check, so `curl` must be available where
the compiled factory runs.

## Compile and apply

Run from this directory so the default `app-source-dir: 'app'` path resolves:

```
unobin compile -o ./build --build
./build/lambdamicrovms-http pin -c dev.ub
./build/lambdamicrovms-http plan -c dev.ub -o plan.json
./build/lambdamicrovms-http apply plan.json
./build/lambdamicrovms-http output -c dev.ub
```

Destroy the persistent resources when finished:

```
./build/lambdamicrovms-http plan -c dev.ub --destroy -o destroy-plan.json
./build/lambdamicrovms-http apply destroy-plan.json
```

Each apply starts a new MicroVM because `run-microvm` uses `@trigger: 'always'`.
The MicroVM is also given a maximum lifetime of ten minutes.
