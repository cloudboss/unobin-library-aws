package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/stretchr/testify/require"
)

func TestVerifyArchive(t *testing.T) {
	out := &eventbridge.DescribeArchiveOutput{
		ArchiveName:    aws.String(archiveName),
		ArchiveArn:     aws.String("arn:aws:events:us-east-1:123456789012:archive/" + archiveName),
		EventSourceArn: aws.String("arn:aws:events:us-east-1:123456789012:event-bus/" + busName),
		Description:    aws.String("initial archive"), RetentionDays: aws.Int32(1),
		EventPattern: aws.String(`{"source":["unobin.initial"]}`),
	}
	require.NoError(t, verifyArchive(out, "applied"))
	require.Error(t, verifyArchive(out, "updated"))
	out.Description, out.RetentionDays = aws.String("updated archive"), aws.Int32(7)
	out.EventPattern = aws.String(`{"source":["unobin.updated"]}`)
	require.NoError(t, verifyArchive(out, "updated"))
	out.EventPattern = aws.String(`{"source":["wrong"]}`)
	require.Error(t, verifyArchive(out, "updated"))
}
