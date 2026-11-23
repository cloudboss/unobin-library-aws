package fixture

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const Marker = "vpc-peering-connection-accepter-it"

func Client(ctx context.Context) (*ec2.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return ec2.NewFromConfig(cfg), nil
}

func Find(ctx context.Context, client *ec2.Client) (*types.VpcPeeringConnection, error) {
	pages := ec2.NewDescribeVpcPeeringConnectionsPaginator(client,
		&ec2.DescribeVpcPeeringConnectionsInput{})
	var found *types.VpcPeeringConnection
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, conn := range page.VpcPeeringConnections {
			if Tag(conn.Tags, "unobin") != Marker {
				continue
			}
			if conn.Status != nil && conn.Status.Code == types.VpcPeeringConnectionStateReasonCodeDeleted {
				continue
			}
			if found != nil {
				return nil, fmt.Errorf("multiple peering fixtures with marker %s", Marker)
			}
			found = &conn
		}
	}
	if found == nil {
		return nil, fmt.Errorf("peering fixture %s was not found", Marker)
	}
	return found, nil
}

func Tag(tags []types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}
