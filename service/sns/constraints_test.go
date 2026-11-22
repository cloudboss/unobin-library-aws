package sns

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestTopicNameConstraints(t *testing.T) {
	schema := readLibrarySchema(t).Resources["topic"]
	inputs := map[string]any{"name": strings.Repeat("t", 256)}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["name"], inputs["fifo-topic"] = "topic.fifo", false
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "standard topic name")
	inputs["name"], inputs["fifo-topic"] = strings.Repeat("t", 251)+".fifo", true
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["name"] = "topic"
	errs = testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "FIFO topic name")
}
