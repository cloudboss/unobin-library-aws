package cognitoidp_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cognitoidp"
	awscognitoidp "github.com/cloudboss/unobin-library-aws/service/cognitoidp"
)

func TestLibraryRegistersUserPoolResource(t *testing.T) {
	library := awscognitoidp.Library()
	require.NotNil(t, library)
	assert.Equal(t, "aws-cognitoidp", library.Name)
	require.NotNil(t, library.Configuration)
	assert.Equal(t, reflect.TypeFor[*awscfg.Configuration](),
		library.Configuration.ValueType())
	require.Len(t, library.Resources, 1)
	require.Contains(t, library.Resources, "user-pool")
	assert.Equal(t, reflect.TypeFor[*svc.UserPoolResourceOutput](),
		library.Resources["user-pool"].OutputType())
	assert.Empty(t, library.DataSources)
	assert.Empty(t, library.Actions)
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awscognitoidp.Library().Configuration)
	require.NoError(t, err)
	assert.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	assert.NotEmpty(t, view.SchemaDigest)
}
