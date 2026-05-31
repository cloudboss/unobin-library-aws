// Package library exports the unobin registration record for the AWS
// library. Library returns the resources, data sources, actions, and
// configuration the library provides, keyed by the names stack source
// uses to reference them.
package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"

	"github.com/cloudboss/unobin-library-aws/library/config"
	"github.com/cloudboss/unobin-library-aws/library/resources"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws",
		Description: "AWS library for unobin.",
		Configuration: &cfg.ConfigurationType{
			Description: "AWS library configuration",
			New:         func() any { return &config.Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"ec2-vpc": runtime.MakeResource[resources.Ec2Vpc, *resources.Ec2VpcOutput](),
		},
		DataSources:   map[string]runtime.DataSourceRegistration{},
		Actions:       map[string]runtime.ActionRegistration{},
		StateBackends: map[string]state.BackendType{},
		Encrypters:    map[string]encrypt.EncrypterType{},
	}
}
