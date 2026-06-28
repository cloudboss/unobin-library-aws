# AWS library

The AWS library provides Unobin resources, data sources, and actions backed by
AWS services. Import it in factory source and pass one AWS configuration value to
the import alias.

```
factory: {
  description: 'Creates one S3 bucket.'

  inputs: {
    aws-config: {
      type: library-config('github.com/cloudboss/unobin-library-aws')
      default: { region: 'us-east-1' }
    }
    bucket-name: { type: string }
  }

  imports: { aws: 'github.com/cloudboss/unobin-library-aws' }

  library-configs: {
    aws: input.aws-config
  }

  resources: {
    assets: aws.s3-bucket {
      bucket: input.bucket-name
      tags: { service: 'assets' }
    }
  }

  outputs: {
    bucket-arn: { value: resource.assets.arn }
  }
}
```

Add the library to the dependency project before compiling the factory:

```
unobin deps get github.com/cloudboss/unobin-library-aws@v0.1.0-a.5
```

## Configuration

Configuration uses the same AWS SDK credential chain as AWS SDK for Go v2: the
environment, shared config and credentials files, SSO, web identity, container
credentials, and IMDS. Static credentials are not fields in the library config.
Use profiles, role assumption, web identity, or the SDK environment variables.

A stack file usually supplies the config as a factory input. The same value can
also be used by the S3 state backend and the KMS encrypter.

```
stack: {
  locals: {
    aws-config: {
      region:  'us-east-1'
      profile: 'dev'
    }
  }

  factory: {
    inputs: {
      aws-config: local.aws-config
      bucket-name: 'acme-dev-assets'
    }
  }

  state: s3 {
    bucket: 'acme-unobin-state'
    prefix: 'assets/dev'
    aws:    local.aws-config
  }

  encryption: kms {
    key-id: 'alias/unobin-state'
    aws:    local.aws-config
  }
}
```

Endpoint settings can target local emulators or S3-compatible object stores:

```
aws-config: {
  region: 'us-east-1'
  endpoint-url: 'http://localhost:4566'
  endpoints: {
    s3:  'http://localhost:4566'
    sts: 'http://localhost:4566'
    kms: 'http://localhost:4566'
  }
}
```

See [configuration reference](reference/configuration.md) for every field.

## Reference

The generated reference lists every library kind, its inputs, outputs,
defaults, constraints, and sensitive fields.

- [Resources](reference/resources/)
- [Data sources](reference/data-sources/)
- [Actions](reference/actions/)
