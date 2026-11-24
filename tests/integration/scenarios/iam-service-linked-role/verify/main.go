package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type identity struct{ ARN, Name, ID string }

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := iam.NewFromConfig(cfg)
	dir := os.Getenv("VERIFY_BUILD_DIR")
	identityPath := filepath.Join(dir, "service-linked-role.json")
	switch os.Getenv("VERIFY_PHASE") {
	case "applied":
		store, err := local.NewStore(filepath.Join(dir, ".unobin/state"),
			"iam-service-linked-role", "config", encrypters.Noop{})
		if err != nil {
			return err
		}
		snapshot, err := store.Current()
		if err != nil {
			return err
		}
		entry := snapshot.Find("resource.linked")
		if entry == nil {
			return errors.New("state has no service-linked role")
		}
		var role identity
		role.ARN, _ = entry.Outputs["arn"].(string)
		role.Name, _ = entry.Outputs["role-name"].(string)
		role.ID, _ = entry.Outputs["role-id"].(string)
		if role.ARN == "" {
			return errors.New("state has no persisted service-linked role ARN")
		}
		if err := verifyRole(ctx, client, role, "integration role",
			map[string]string{"change": "old", "keep": "1", "remove": "yes"}); err != nil {
			return err
		}
		data, err := json.Marshal(role)
		if err != nil {
			return err
		}
		return os.WriteFile(identityPath, data, 0o600)
	case "updated", "destroyed":
		data, err := os.ReadFile(identityPath)
		if err != nil {
			return err
		}
		var role identity
		if err := json.Unmarshal(data, &role); err != nil {
			return err
		}
		if os.Getenv("VERIFY_PHASE") == "updated" {
			return verifyRole(ctx, client, role, "updated integration role",
				map[string]string{"add": "", "change": "new", "keep": "1"})
		}
		_, err = client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(role.Name)})
		var missing *types.NoSuchEntityException
		if errors.As(err, &missing) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("service-linked role %s still exists", role.ARN)
	default:
		return fmt.Errorf("unknown verification step %q", os.Getenv("VERIFY_PHASE"))
	}
}

func verifyRole(ctx context.Context, client *iam.Client, identity identity,
	description string, wantTags map[string]string) error {
	result, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(identity.Name)})
	if err != nil {
		return err
	}
	role := result.Role
	if role == nil || aws.ToString(role.Arn) != identity.ARN ||
		aws.ToString(role.RoleId) != identity.ID {
		return fmt.Errorf("service-linked role identity changed: got %+v, want %+v", role, identity)
	}
	if aws.ToString(role.Description) != description {
		return fmt.Errorf("description is %q, want %q", aws.ToString(role.Description), description)
	}
	tags := map[string]string{}
	pager := iam.NewListRoleTagsPaginator(client, &iam.ListRoleTagsInput{
		RoleName: aws.String(identity.Name),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, tag := range page.Tags {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	if !maps.Equal(tags, wantTags) {
		return fmt.Errorf("tags are %v, want %v", tags, wantTags)
	}
	return nil
}
