package sqs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestQueueNameConstraints(t *testing.T) {
	schema := readLibrarySchema(t).Resources["queue"]
	inputs := map[string]any{"name": strings.Repeat("q", 80)}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["name"] = "queue.fifo"
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "standard queue name")
	inputs["fifo-queue"] = true
	inputs["name"] = strings.Repeat("q", 75) + ".fifo"
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["name"] = strings.Repeat("q", 76) + ".fifo"
	errs = testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "FIFO queue name")
}
