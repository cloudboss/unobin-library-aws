package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const projectName = "unobin-it-codebuild-project"
const roleName = "unobin-it-codebuild-role"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := sdk.NewFromConfig(cfg)
	result, err := client.BatchGetProjects(ctx,
		&sdk.BatchGetProjectsInput{Names: []string{projectName}})
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	if result == nil {
		return errors.New("get project returned no response")
	}
	if os.Getenv("VERIFY_PHASE") == "destroyed" {
		if len(result.Projects) != 0 {
			return errors.New("project still exists")
		}
		_, err := iam.NewFromConfig(cfg).GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
		var absent *iamtypes.NoSuchEntityException
		if !errors.As(err, &absent) {
			return fmt.Errorf("role cleanup: expected absence, got %v", err)
		}
		return nil
	}
	description, timeout := "initial", int32(60)
	tags := map[string]string{"keep": "1", "change": "old", "remove": "yes"}
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
	case "updated":
		description, timeout = "updated", 90
		tags = map[string]string{"keep": "1", "change": "new", "add": "yes"}
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
	if len(result.Projects) != 1 {
		return fmt.Errorf("expected one project, got %d", len(result.Projects))
	}
	p := result.Projects[0]
	if aws.ToString(p.Name) != projectName || aws.ToString(p.Arn) == "" ||
		aws.ToString(p.Description) != description || aws.ToInt32(p.TimeoutInMinutes) != timeout {
		return fmt.Errorf("unexpected project values: %#v", p)
	}
	if p.Environment == nil || len(p.Environment.EnvironmentVariables) != 1 ||
		aws.ToString(p.Environment.EnvironmentVariables[0].Name) != "PHASE" ||
		aws.ToString(p.Environment.EnvironmentVariables[0].Value) != description {
		return fmt.Errorf("unexpected environment: %#v", p.Environment)
	}
	gotTags := map[string]string{}
	for _, tag := range p.Tags {
		gotTags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if !maps.Equal(tags, gotTags) {
		return fmt.Errorf("tags: got %v, want %v", gotTags, tags)
	}
	role, err := iam.NewFromConfig(cfg).GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
	if err != nil {
		return fmt.Errorf("get managed role: %w", err)
	}
	if role == nil || role.Role == nil || aws.ToString(role.Role.Arn) != aws.ToString(p.ServiceRole) {
		return errors.New("project does not use the managed role")
	}
	return nil
}
