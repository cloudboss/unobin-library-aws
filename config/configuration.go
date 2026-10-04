package config

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "aws-config",
		Description: "AWS configuration library for Unobin.",
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI:            "1.0",
			SuggestedUnobinVersion: "v0.12.0",
		},
		Configuration: LibraryConfiguration(),
	}
}

func LibraryConfiguration() *cfg.ConfigurationType[*awscfg.Configuration] {
	return &cfg.ConfigurationType[*awscfg.Configuration]{
		Description: "AWS library configuration",
		New:         func() *awscfg.Configuration { return &awscfg.Configuration{} },
	}
}
