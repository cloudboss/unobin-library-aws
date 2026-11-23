package eventbridge

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type PutEventsAction struct {
	Entries *[]PutEventEntry `ub:"entries"`
	Region  *string          `ub:"region"`
}

type PutEventEntry struct {
	Source       string    `ub:"source"`
	Detail       *string   `ub:"detail"`
	DetailType   *string   `ub:"detail-type"`
	EventBusName *string   `ub:"event-bus-name"`
	Resources    *[]string `ub:"resources"`
	Time         *string   `ub:"time"`
}

type PutEventsActionOutput struct{}

type putEventsClient interface {
	PutEvents(
		context.Context, *eventbridge.PutEventsInput, ...func(*eventbridge.Options),
	) (*eventbridge.PutEventsOutput, error)
}

func (r PutEventsAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(r.Entries, func(entry PutEventEntry) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.NotEquals(entry.Source, nil)).
					Message("source is required"),
				constraint.Must(constraint.IsFormat(entry.Time, constraint.FormatDateTime)).
					Message("time must be an RFC3339 timestamp"),
				constraint.ForEach(entry.Resources, func(resource string) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(constraint.NotEquals(resource, nil)).
							Message("resources must not contain null"),
					}
				}),
			}
		}),
	}
}

func (r *PutEventsAction) Run(ctx context.Context, cfg *awsCfg) (*PutEventsActionOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if region := aws.ToString(r.Region); region != "" {
		configuredPartition := eventRegionPartition(sdkConfig.Region)
		if partition := eventRegionPartition(region); partition != configuredPartition {
			return nil, fmt.Errorf("region %q belongs to partition %q, expected %q",
				region, partition, configuredPartition)
		}
		sdkConfig.Region = region
	}
	return r.run(ctx, newClientFromConfig(sdkConfig))
}

func (r *PutEventsAction) run(
	ctx context.Context, client putEventsClient,
) (*PutEventsActionOutput, error) {
	request, err := r.prepareRequest()
	if err != nil {
		return nil, err
	}
	response, err := client.PutEvents(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("put EventBridge events: %w", err)
	}
	if response.FailedEntryCount > 0 {
		return nil, fmt.Errorf("%d entries failed to be processed", response.FailedEntryCount)
	}
	return &PutEventsActionOutput{}, nil
}

func (r *PutEventsAction) prepareRequest() (*eventbridge.PutEventsInput, error) {
	var entries []PutEventEntry
	if r.Entries != nil {
		entries = *r.Entries
	}
	request := &eventbridge.PutEventsInput{
		Entries: make([]types.PutEventsRequestEntry, 0, len(entries)),
	}
	for i, entry := range entries {
		event := types.PutEventsRequestEntry{
			Source:       aws.String(entry.Source),
			Detail:       entry.Detail,
			DetailType:   entry.DetailType,
			EventBusName: entry.EventBusName,
		}
		if entry.Resources != nil {
			event.Resources = *entry.Resources
		}
		if entry.Time != nil {
			timestamp, err := time.Parse(time.RFC3339, *entry.Time)
			if err != nil {
				return nil, fmt.Errorf("parse entries[%d].time: %w", i, err)
			}
			event.Time = &timestamp
		}
		request.Entries = append(request.Entries, event)
	}
	return request, nil
}

func eventRegionPartition(region string) string {
	partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if ok {
		return partition.ID()
	}
	return "aws"
}
