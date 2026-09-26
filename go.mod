module github.com/cloudboss/unobin-library-aws

go 1.26.2

require (
	github.com/ProtonMail/go-crypto v1.4.1
	github.com/aws/aws-sdk-go-v2 v1.42.1
	github.com/aws/aws-sdk-go-v2/config v1.32.18
	github.com/aws/aws-sdk-go-v2/service/acm v1.38.3
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.34.4
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.66.2
	github.com/aws/aws-sdk-go-v2/service/cloudfront v1.64.0
	github.com/aws/aws-sdk-go-v2/service/cloudtrail v1.56.4
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.52.8
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.74.0
	github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider v1.62.0
	github.com/aws/aws-sdk-go-v2/service/dsql v1.14.6
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.57.4
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.303.0
	github.com/aws/aws-sdk-go-v2/service/ecr v1.57.2
	github.com/aws/aws-sdk-go-v2/service/ecs v1.80.0
	github.com/aws/aws-sdk-go-v2/service/efs v1.42.1
	github.com/aws/aws-sdk-go-v2/service/eks v1.87.0
	github.com/aws/aws-sdk-go-v2/service/elasticache v1.54.3
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.54.12
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.45.25
	github.com/aws/aws-sdk-go-v2/service/iam v1.53.10
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.45.1
	github.com/aws/aws-sdk-go-v2/service/kms v1.52.0
	github.com/aws/aws-sdk-go-v2/service/lambda v1.90.1
	github.com/aws/aws-sdk-go-v2/service/lambdamicrovms v1.0.0
	github.com/aws/aws-sdk-go-v2/service/opensearch v1.72.0
	github.com/aws/aws-sdk-go-v2/service/rds v1.118.2
	github.com/aws/aws-sdk-go-v2/service/route53 v1.62.7
	github.com/aws/aws-sdk-go-v2/service/s3 v1.101.0
	github.com/aws/aws-sdk-go-v2/service/scheduler v1.18.7
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.7
	github.com/aws/aws-sdk-go-v2/service/sfn v1.43.0
	github.com/aws/aws-sdk-go-v2/service/sns v1.39.17
	github.com/aws/aws-sdk-go-v2/service/sqs v1.42.27
	github.com/aws/aws-sdk-go-v2/service/ssm v1.68.6
	github.com/aws/aws-sdk-go-v2/service/sts v1.42.1
	github.com/aws/aws-sdk-go-v2/service/wafv2 v1.73.0
	github.com/aws/smithy-go v1.27.3
	github.com/cloudboss/unobin v0.12.0-a.6
	github.com/hashicorp/aws-sdk-go-base/v2 v2.0.0-beta.73
	github.com/hashicorp/awspolicyequivalence v1.7.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/net v0.56.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	dario.cat/mergo v1.0.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.14 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.17 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.12.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.9.0 // indirect
	github.com/go-git/go-git/v5 v5.19.2 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.16 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/pjbgf/sha1cd v0.6.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3 // indirect
	github.com/skeema/knownhosts v1.3.1 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/api v0.286.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260610212136-7ab31c22f7ad // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
)
