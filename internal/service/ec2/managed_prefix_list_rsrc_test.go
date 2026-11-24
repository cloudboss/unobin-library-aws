package ec2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
)

func TestManagedPrefixListPrepareAndSetIdentity(t *testing.T) {
	entries := []ManagedPrefixListEntry{
		{Cidr: "2001:DB8::1/64", Description: aws.String("")},
		{Cidr: "10.0.0.1/8", Description: aws.String("two")},
		{Cidr: "10.0.0.1/8"}, {Cidr: "10.0.0.1/8", Description: aws.String("")},
	}
	r := ManagedPrefixListResource{AddressFamily: "IPv4", MaxEntries: 2147483648,
		Name: "aws:allowed locally", Entries: &entries,
		Tags: &map[string]string{"blank": "", "aws:reserved": "ignored"}}
	assert.Equal(t, &ec2.CreateManagedPrefixListInput{
		AddressFamily: aws.String("IPv4"), MaxEntries: aws.Int32(-2147483648),
		PrefixListName: aws.String(r.Name), ClientToken: aws.String("token"),
		Entries: []ec2types.AddPrefixListEntry{
			{Cidr: aws.String("10.0.0.1/8")},
			{Cidr: aws.String("10.0.0.1/8"), Description: aws.String("two")},
			{Cidr: aws.String("2001:DB8::1/64")},
		},
		TagSpecifications: []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypePrefixList,
			Tags: []ec2types.Tag{{Key: aws.String("blank"), Value: aws.String("")}}}},
	}, r.prepareRequest("token"))
	assert.True(t, managedPrefixListEntriesEqual(&entries,
		&[]ManagedPrefixListEntry{entries[2], entries[1], entries[0]}))
	assert.True(t, managedPrefixListEntriesEqual(&entries, nil))
	assert.False(t, managedPrefixListEntriesEqual(nil, &[]ManagedPrefixListEntry{}))
	assert.False(t, managedPrefixListEntriesEqual(&entries, &[]ManagedPrefixListEntry{}))
	for _, collection := range []*[]ManagedPrefixListEntry{nil, {}} {
		r.Entries, r.Tags = collection, nil
		assert.Nil(t, r.prepareRequest("").Entries)
		assert.Nil(t, r.prepareRequest("").TagSpecifications)
	}
}
