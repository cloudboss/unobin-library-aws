package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const project = "unobin-it-start-build"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err == nil {
		err = verify(ctx, sdk.NewFromConfig(cfg), iam.NewFromConfig(cfg), os.Getenv("VERIFY_PHASE"))
	}
	if err != nil {
		log.Fatal(err)
	}
}

func verify(ctx context.Context, client *sdk.Client, roles *iam.Client, phase string) error {
	if phase != "applied" && phase != "updated" && phase != "destroyed" {
		return fmt.Errorf("unexpected phase %q", phase)
	}
	projects, err := client.BatchGetProjects(ctx, &sdk.BatchGetProjectsInput{Names: []string{project}})
	if err != nil {
		return err
	}
	if phase == "destroyed" {
		if len(projects.Projects) != 0 {
			return errors.New("owned project still exists")
		}
		_, err := roles.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(project + "-role")})
		var missing *iamtypes.NoSuchEntityException
		if !errors.As(err, &missing) {
			return fmt.Errorf("owned role still exists: %v", err)
		}
		fmt.Println("ok: project and role destroyed")
		return nil
	}
	if len(projects.Projects) != 1 || aws.ToString(projects.Projects[0].Name) != project {
		return errors.New("expected owned project")
	}
	request := &sdk.ListBuildsForProjectInput{ProjectName: aws.String(project)}
	var values []string
	for {
		page, err := client.ListBuildsForProject(ctx, request)
		if err != nil {
			return err
		}
		for _, id := range page.Ids {
			out, err := client.BatchGetBuilds(ctx, &sdk.BatchGetBuildsInput{Ids: []string{id}})
			if err != nil {
				return err
			}
			if len(out.Builds) != 1 {
				return errors.New("expected one owned build")
			}
			build := out.Builds[0]
			if aws.ToString(build.Id) != id || aws.ToString(build.ProjectName) != project ||
				build.BuildStatus != types.StatusTypeSucceeded || build.Environment == nil {
				return errors.New("owned build identity or status differs")
			}
			vars := build.Environment.EnvironmentVariables
			if len(vars) != 1 || aws.ToString(vars[0].Name) != "RUN" || vars[0].Type != "PLAINTEXT" {
				return errors.New("owned build environment differs")
			}
			values = append(values, aws.ToString(vars[0].Value))
		}
		if aws.ToString(page.NextToken) == "" {
			break
		}
		request.NextToken = page.NextToken
	}
	want := []string{"applied"}
	if phase == "updated" {
		want = append(want, "updated")
	}
	slices.Sort(values)
	if !slices.Equal(want, values) {
		return fmt.Errorf("build overrides %v; want %v", values, want)
	}
	fmt.Printf("ok: %s builds %v\n", phase, values)
	return nil
}
