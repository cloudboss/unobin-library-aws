package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

const cluster = "unobin-it-eks-pod-id"

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("verify EKS Pod Identity association: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	client := eks.NewFromConfig(cfg)
	path := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "pod-identity-association-id")
	phase := os.Getenv("VERIFY_PHASE")
	var id string
	switch phase {
	case "applied":
		list, err := client.ListPodIdentityAssociations(ctx, &eks.ListPodIdentityAssociationsInput{
			ClusterName: aws.String(cluster), Namespace: aws.String("unobin"),
			ServiceAccount: aws.String("workload"),
		})
		if err != nil {
			return err
		}
		if len(list.Associations) != 1 {
			return fmt.Errorf("expected one association, got %+v", list.Associations)
		}
		id = aws.ToString(list.Associations[0].AssociationId)
		if id == "" {
			return errors.New("association has no ID")
		}
		if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
			return err
		}
	case "updated", "destroyed":
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		id = string(data)
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}

	response, err := client.DescribePodIdentityAssociation(ctx,
		&eks.DescribePodIdentityAssociationInput{
			ClusterName: aws.String(cluster), AssociationId: aws.String(id),
		},
	)
	if phase == "destroyed" {
		var missing *ekstypes.ResourceNotFoundException
		if errors.As(err, &missing) {
			return nil
		}
		if err == nil {
			return fmt.Errorf("association %s still exists", id)
		}
	}
	if err != nil {
		return err
	}
	return verifyAssociation(response.Association, id, phase == "updated")
}

func verifyAssociation(
	association *ekstypes.PodIdentityAssociation, id string, updated bool,
) error {
	if association == nil {
		return errors.New("describe returned no association")
	}
	arn, err := awsarn.Parse(aws.ToString(association.AssociationArn))
	if err != nil {
		return fmt.Errorf("parse association ARN: %w", err)
	}
	suffix := ""
	if updated {
		suffix = "-next"
	}
	role := fmt.Sprintf("arn:%s:iam::%s:role/%s-workload%s", arn.Partition, arn.AccountID,
		cluster, suffix)
	target := fmt.Sprintf("arn:%s:iam::%s:role/%s-target%s", arn.Partition, arn.AccountID,
		cluster, suffix)
	if aws.ToString(association.AssociationId) != id ||
		arn.Resource != "podidentityassociation/"+cluster+"/"+id ||
		aws.ToString(association.ClusterName) != cluster ||
		aws.ToString(association.Namespace) != "unobin" ||
		aws.ToString(association.ServiceAccount) != "workload" ||
		aws.ToString(association.RoleArn) != role ||
		aws.ToString(association.TargetRoleArn) != target ||
		aws.ToBool(association.DisableSessionTags) != updated ||
		!maps.Equal(association.Tags, map[string]string{
			"scenario": "pod-identity-association", "empty": "",
		}) {
		return fmt.Errorf("unexpected association %s (%s): cluster=%s namespace=%s account=%s "+
			"role=%s target=%s disabled=%t tags=%v",
			aws.ToString(association.AssociationId), arn.Resource,
			aws.ToString(association.ClusterName), aws.ToString(association.Namespace),
			aws.ToString(association.ServiceAccount), aws.ToString(association.RoleArn),
			aws.ToString(association.TargetRoleArn), aws.ToBool(association.DisableSessionTags),
			association.Tags)
	}
	return nil
}
