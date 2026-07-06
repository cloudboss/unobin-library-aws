package service_test

import (
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/meta"
	"github.com/cloudboss/unobin-library-aws/service/acm"
	"github.com/cloudboss/unobin-library-aws/service/apigatewayv2"
	"github.com/cloudboss/unobin-library-aws/service/autoscaling"
	"github.com/cloudboss/unobin-library-aws/service/cloudfront"
	"github.com/cloudboss/unobin-library-aws/service/cloudwatch"
	"github.com/cloudboss/unobin-library-aws/service/cloudwatchlogs"
	"github.com/cloudboss/unobin-library-aws/service/dynamodb"
	"github.com/cloudboss/unobin-library-aws/service/ec2"
	"github.com/cloudboss/unobin-library-aws/service/ecr"
	"github.com/cloudboss/unobin-library-aws/service/ecs"
	"github.com/cloudboss/unobin-library-aws/service/elbv2"
	"github.com/cloudboss/unobin-library-aws/service/eventbridge"
	"github.com/cloudboss/unobin-library-aws/service/iam"
	"github.com/cloudboss/unobin-library-aws/service/kms"
	awslambda "github.com/cloudboss/unobin-library-aws/service/lambda"
	"github.com/cloudboss/unobin-library-aws/service/lambdamicrovms"
	"github.com/cloudboss/unobin-library-aws/service/rds"
	"github.com/cloudboss/unobin-library-aws/service/route53"
	"github.com/cloudboss/unobin-library-aws/service/s3"
	"github.com/cloudboss/unobin-library-aws/service/secretsmanager"
	"github.com/cloudboss/unobin-library-aws/service/sns"
	"github.com/cloudboss/unobin-library-aws/service/sqs"
	"github.com/cloudboss/unobin-library-aws/service/ssm"
	"github.com/cloudboss/unobin-library-aws/service/sts"
)

func TestPublicKindNames(t *testing.T) {
	for name, lib := range libraries() {
		t.Run(name, func(t *testing.T) {
			for kind := range lib.Resources {
				if strings.HasSuffix(kind, "-resource") {
					t.Errorf("resource kind %q has a category suffix", kind)
				}
			}
			for kind := range lib.DataSources {
				if strings.HasSuffix(kind, "-data") {
					t.Errorf("data source kind %q has a category suffix", kind)
				}
			}
			for kind := range lib.Actions {
				if strings.HasSuffix(kind, "-action") {
					t.Errorf("action kind %q has a category suffix", kind)
				}
			}
		})
	}
}

func libraries() map[string]*runtime.Library {
	return map[string]*runtime.Library{
		"meta":           meta.Library(),
		"acm":            acm.Library(),
		"apigatewayv2":   apigatewayv2.Library(),
		"autoscaling":    autoscaling.Library(),
		"cloudfront":     cloudfront.Library(),
		"cloudwatch":     cloudwatch.Library(),
		"cloudwatchlogs": cloudwatchlogs.Library(),
		"dynamodb":       dynamodb.Library(),
		"ec2":            ec2.Library(),
		"ecr":            ecr.Library(),
		"ecs":            ecs.Library(),
		"elbv2":          elbv2.Library(),
		"eventbridge":    eventbridge.Library(),
		"iam":            iam.Library(),
		"kms":            kms.Library(),
		"lambda":         awslambda.Library(),
		"lambdamicrovms": lambdamicrovms.Library(),
		"rds":            rds.Library(),
		"route53":        route53.Library(),
		"s3":             s3.Library(),
		"secretsmanager": secretsmanager.Library(),
		"sns":            sns.Library(),
		"sqs":            sqs.Library(),
		"ssm":            ssm.Library(),
		"sts":            sts.Library(),
	}
}
