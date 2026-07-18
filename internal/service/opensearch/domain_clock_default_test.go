package opensearch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDomainClockIsReal(t *testing.T) {
	assert.IsType(t, realDomainClock{}, newDomainClock())
	assert.IsType(t, realDomainClock{}, defaultDomainOperationOptions().clock)
}

func TestDefaultDomainClockIgnoresEndpointEnvironment(t *testing.T) {
	variables := []string{
		"AWS_ENDPOINT_URL",
		"AWS_ENDPOINT_URL_OPENSEARCH",
		"LOCALSTACK_ENDPOINT",
		"MINISTACK_ENDPOINT",
	}
	for _, variable := range variables {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(variable, "http://127.0.0.1:1")
			assert.IsType(t, realDomainClock{}, newDomainClock())
			assert.IsType(t, realDomainClock{}, defaultDomainOperationOptions().clock)
		})
	}
}
