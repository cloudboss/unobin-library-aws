package opensearch

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type domainPlanProbe DomainResource

func (*domainPlanProbe) SchemaVersion() int { return 1 }

func (*domainPlanProbe) Create(context.Context, any) (map[string]any, error) {
	return map[string]any{"arn": "arn:new"}, nil
}

func (*domainPlanProbe) Read(
	_ context.Context,
	_ any,
	prior map[string]any,
) (map[string]any, error) {
	return prior, nil
}

func (*domainPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[domainPlanProbe, map[string]any],
) (map[string]any, error) {
	return prior.Outputs, nil
}

func (*domainPlanProbe) Delete(context.Context, any, map[string]any) error { return nil }

func (*domainPlanProbe) ReplaceFields() []string {
	return (&DomainResource{}).ReplaceFields()
}

func (*domainPlanProbe) EquivalentInput(
	field string,
	prior domainPlanProbe,
	current domainPlanProbe,
) bool {
	return (&DomainResource{}).EquivalentInput(
		field,
		DomainResource(prior),
		DomainResource(current),
	)
}

func TestDomainRuntimePlanTreatsConfiguredCollectionsAsUnordered(t *testing.T) {
	tests := []struct {
		name   string
		prior  map[string]any
		source string
	}{
		{
			name: "VPC IDs",
			prior: map[string]any{
				"domain-name": "example",
				"vpc-options": map[string]any{
					"egress-enabled":     true,
					"security-group-ids": []any{"sg-a", "sg-b"},
					"subnet-ids":         []any{"subnet-a", "subnet-b"},
				},
			},
			source: `factory: {
  resources: {
    domain: aws-opensearch.domain {
      domain-name: 'example'
      vpc-options: {
        egress-enabled:     true
        security-group-ids: ['sg-b', 'sg-a']
        subnet-ids:         ['subnet-b', 'subnet-a']
      }
    }
  }
}`,
		},
		{
			name: "Auto-Tune schedules",
			prior: map[string]any{
				"domain-name": "example",
				"auto-tune-options": map[string]any{
					"desired-state": "ENABLED",
					"maintenance-schedule": []any{
						domainPlanSchedule("2026-07-18T12:00:00Z"),
						domainPlanSchedule("2026-07-19T12:00:00Z"),
					},
					"use-off-peak-window": false,
				},
			},
			source: `factory: {
  resources: {
    domain: aws-opensearch.domain {
      domain-name: 'example'
      auto-tune-options: {
        desired-state: 'ENABLED'
        maintenance-schedule: [
          {
            cron-expression-for-recurrence: 'cron(0 12 ? * SUN *)'
            duration: { unit: 'HOURS', value: 2 }
            start-at: '2026-07-19T12:00:00Z'
          },
          {
            cron-expression-for-recurrence: 'cron(0 12 ? * SUN *)'
            duration: { unit: 'HOURS', value: 2 }
            start-at: '2026-07-18T12:00:00Z'
          },
        ]
        use-off-peak-window: false
      }
    }
  }
}`,
		},
		{
			name: "log entries",
			prior: map[string]any{
				"domain-name": "example",
				"log-publishing-options": []any{
					domainPlanLog("INDEX_SLOW_LOGS", "first"),
					domainPlanLog("SEARCH_SLOW_LOGS", "second"),
				},
			},
			source: `factory: {
  resources: {
    domain: aws-opensearch.domain {
      domain-name: 'example'
      log-publishing-options: [
        {
          cloudwatch-log-group-arn: 'second'
          enabled:                  true
          log-type:                 'SEARCH_SLOW_LOGS'
        },
        {
          cloudwatch-log-group-arn: 'first'
          enabled:                  true
          log-type:                 'INDEX_SLOW_LOGS'
        },
      ]
    }
  }
}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			step := planDomainChange(t, test.prior, test.source)

			assert.Equal(t, runtime.DecisionNoOp, step.Decision)
			assert.Empty(t, step.ReplaceTriggers)
		})
	}
}

func TestDomainRuntimePlanReplacesChangedVPCMember(t *testing.T) {
	prior := map[string]any{
		"domain-name": "example",
		"vpc-options": map[string]any{
			"security-group-ids": []any{"sg-a", "sg-b"},
			"subnet-ids":         []any{"subnet-a", "subnet-b"},
		},
	}
	source := `factory: {
  resources: {
    domain: aws-opensearch.domain {
      domain-name: 'example'
      vpc-options: {
        security-group-ids: ['sg-b', 'sg-c']
        subnet-ids:         ['subnet-b', 'subnet-a']
      }
    }
  }
}`

	step := planDomainChange(t, prior, source)

	assert.Equal(t, runtime.DecisionReplace, step.Decision)
	assert.Equal(t, []string{"vpc-options"}, step.ReplaceTriggers)
}

func planDomainChange(
	t *testing.T,
	prior map[string]any,
	source string,
) *runtime.PlanStep {
	t.Helper()
	parsed, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-opensearch": {
			Name: "aws-opensearch",
			Resources: map[string]runtime.ResourceRegistration{
				"domain": runtime.MakeResource[domainPlanProbe, map[string]any, any](),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.domain",
		Type:          state.EntryLeaf,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-opensearch", Export: "domain"},
		SchemaVersion: 1,
		Inputs:        prior,
		Outputs:       map[string]any{"arn": "arn:prior"},
	}}
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor := &runtime.Executor{
		DAG:          runtime.BuildSyntaxDAG(body, libraries),
		SyntaxSource: &body,
		Libraries:    libraries,
		Store:        store,
		Factory:      factory,
	}
	plan, err := executor.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	return plan.Steps[0]
}

func domainPlanSchedule(start string) map[string]any {
	return map[string]any{
		"cron-expression-for-recurrence": "cron(0 12 ? * SUN *)",
		"duration":                       map[string]any{"unit": "HOURS", "value": int64(2)},
		"start-at":                       start,
	}
}

func domainPlanLog(logType, arn string) map[string]any {
	return map[string]any{
		"cloudwatch-log-group-arn": arn,
		"enabled":                  true,
		"log-type":                 logType,
	}
}
