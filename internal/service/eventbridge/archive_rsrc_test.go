package eventbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const archiveSource = "arn:aws:events:us-east-1:123456789012:event-bus/test"

func TestArchivePreparation(t *testing.T) {
	for _, tt := range []struct{ name, pattern, want string }{
		{"empty", "", ""},
		{"normalized", ` { "z": 1.00, "a": "<>&" } `,
			`{"a":"\u003c\u003e\u0026","z":1}`},
		{"float64", `9007199254740993`, `9007199254740992`},
		{"null", `null`, `null`},
		{"scalar", `true`, `true`},
		{"boundary", `"` + strings.Repeat("<", 4094) + `"`,
			`"` + strings.Repeat(`\u003c`, 4094) + `"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := ArchiveResource{Name: "test", EventSourceArn: archiveSource,
				EventPattern: aws.String(tt.pattern)}
			got, err := r.prepareArchive(r.Name)
			require.NoError(t, err)
			want := &eventbridge.CreateArchiveInput{ArchiveName: aws.String("test"),
				EventSourceArn: aws.String(archiveSource)}
			if tt.want != "" {
				want.EventPattern = aws.String(tt.want)
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestArchivePreparationOptions(t *testing.T) {
	for _, days := range []int{0, 30, -1, 1 << 32, 1<<32 + 1} {
		r := ArchiveResource{Name: "test", EventSourceArn: archiveSource,
			Description: aws.String("description"), KmsKeyIdentifier: aws.String("alias/key"),
			RetentionDays: &days}
		got, err := r.prepareArchive("prior")
		require.NoError(t, err)
		want := &eventbridge.CreateArchiveInput{ArchiveName: aws.String("prior"),
			EventSourceArn: aws.String(archiveSource), Description: aws.String("description"),
			KmsKeyIdentifier: aws.String("alias/key")}
		if days != 0 {
			want.RetentionDays = aws.Int32(int32(days))
		}
		assert.Equal(t, want, got)
	}
	r := ArchiveResource{Name: "test", Description: aws.String(""),
		KmsKeyIdentifier: aws.String(""), EventPattern: aws.String("")}
	got, err := r.prepareArchive(r.Name)
	require.NoError(t, err)
	assert.Equal(t, &eventbridge.CreateArchiveInput{
		ArchiveName: aws.String("test"), EventSourceArn: aws.String("")}, got)
}

func TestArchivePreparationErrors(t *testing.T) {
	for _, source := range []string{"missing", "arn:aws123:events:::x",
		"arn:aws:events:useast1::x", "arn:aws:events::123:x", "arn:aws:events:::"} {
		r := ArchiveResource{Name: "test", EventSourceArn: source}
		_, err := r.prepareArchive(r.Name)
		require.ErrorContains(t, err, "event-source-arn")
	}
	for _, source := range []string{"", "arn:aws:::partner-managed:x:y",
		"arn:aws-us-gov:any:us-gov-west-1:cw1234567890:x"} {
		r := ArchiveResource{Name: "test", EventSourceArn: source}
		_, err := r.prepareArchive(r.Name)
		require.NoError(t, err)
	}
	for _, pattern := range []string{"{", "{} {}", `"` + strings.Repeat("a", 4095) + `"`} {
		r := ArchiveResource{Name: "test", EventPattern: aws.String(pattern)}
		_, err := r.prepareArchive(r.Name)
		require.ErrorContains(t, err, "event-pattern")
	}
}

type archiveFake struct {
	calls                                    []any
	described                                *eventbridge.DescribeArchiveOutput
	createErr, readErr, updateErr, deleteErr error
	onRead                                   func(context.Context)
	onDelete                                 func(context.Context)
}

func (f *archiveFake) CreateArchive(_ context.Context, in *eventbridge.CreateArchiveInput,
	_ ...func(*eventbridge.Options)) (*eventbridge.CreateArchiveOutput, error) {
	f.calls = append(f.calls, in)
	return &eventbridge.CreateArchiveOutput{ArchiveArn: aws.String("ignored")}, f.createErr
}
func (f *archiveFake) DescribeArchive(ctx context.Context, in *eventbridge.DescribeArchiveInput,
	_ ...func(*eventbridge.Options)) (*eventbridge.DescribeArchiveOutput, error) {
	f.calls = append(f.calls, in)
	if f.onRead != nil {
		f.onRead(ctx)
	}
	return f.described, f.readErr
}
func (f *archiveFake) UpdateArchive(_ context.Context, in *eventbridge.UpdateArchiveInput,
	_ ...func(*eventbridge.Options)) (*eventbridge.UpdateArchiveOutput, error) {
	f.calls = append(f.calls, in)
	return &eventbridge.UpdateArchiveOutput{}, f.updateErr
}
func (f *archiveFake) DeleteArchive(ctx context.Context, in *eventbridge.DeleteArchiveInput,
	_ ...func(*eventbridge.Options)) (*eventbridge.DeleteArchiveOutput, error) {
	f.calls = append(f.calls, in)
	if f.onDelete != nil {
		f.onDelete(ctx)
	}
	return &eventbridge.DeleteArchiveOutput{}, f.deleteErr
}

func TestArchiveLifecycle(t *testing.T) {
	r := ArchiveResource{Name: "test", EventSourceArn: archiveSource,
		Description: aws.String("description"), EventPattern: aws.String(` {"x":1} `),
		KmsKeyIdentifier: aws.String("alias/key"), RetentionDays: aws.Int(30)}
	f := &archiveFake{described: &eventbridge.DescribeArchiveOutput{
		ArchiveArn: aws.String("remote-arn"), ArchiveName: aws.String("test"),
		EventSourceArn: aws.String(archiveSource), Description: r.Description,
		EventPattern: aws.String(` {"x":1} `), KmsKeyIdentifier: r.KmsKeyIdentifier,
		RetentionDays: aws.Int32(30)}}
	out, err := r.createArchive(t.Context(), f)
	require.NoError(t, err)
	assert.Equal(t, &ArchiveResourceOutput{ArchiveArn: "remote-arn", ArchiveName: "test",
		EventSourceArn: archiveSource, Description: "description", EventPattern: ` {"x":1} `,
		KmsKeyIdentifier: "alias/key", RetentionDays: 30}, out)
	create, err := r.prepareArchive("test")
	require.NoError(t, err)
	assert.Equal(t, []any{create,
		&eventbridge.DescribeArchiveInput{ArchiveName: aws.String("test")}}, f.calls)
	f.calls = nil
	prior := runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg]{
		Inputs: r, Outputs: out}
	_, err = r.updateArchive(t.Context(), f, prior)
	require.NoError(t, err)
	assert.Empty(t, f.calls)
	observed := *out
	observed.Description = "drift"
	prior.Observed = &observed
	_, err = r.updateArchive(t.Context(), f, prior)
	require.NoError(t, err)
	assert.Equal(t, []any{&eventbridge.UpdateArchiveInput{ArchiveName: aws.String("test"),
		Description: r.Description, EventPattern: aws.String(`{"x":1}`),
		KmsKeyIdentifier: r.KmsKeyIdentifier, RetentionDays: aws.Int32(30)},
		&eventbridge.DescribeArchiveInput{ArchiveName: aws.String("test")}}, f.calls)
	f.calls = nil
	r.Description, r.EventPattern, r.KmsKeyIdentifier, r.RetentionDays = nil, nil, nil, nil
	_, err = r.updateArchive(t.Context(), f, prior)
	require.NoError(t, err)
	assert.Equal(t, []any{&eventbridge.UpdateArchiveInput{ArchiveName: aws.String("test")},
		&eventbridge.DescribeArchiveInput{ArchiveName: aws.String("test")}}, f.calls)
	f.calls = nil
	r.Description = aws.String("new")
	_, err = r.updateArchive(t.Context(), f, prior)
	require.NoError(t, err)
	assert.Equal(t, []any{&eventbridge.UpdateArchiveInput{ArchiveName: aws.String("test"),
		Description: r.Description},
		&eventbridge.DescribeArchiveInput{ArchiveName: aws.String("test")}}, f.calls)
	f.calls = nil
	require.NoError(t, deleteArchive(t.Context(), f, archiveName("desired", out)))
	assert.Equal(t, []any{&eventbridge.DeleteArchiveInput{ArchiveName: aws.String("test")}}, f.calls)
	assert.Equal(t, "desired", archiveName("desired", nil))
}

func TestArchiveAbsenceAndErrors(t *testing.T) {
	failure := errors.New("failure")
	for _, tt := range []struct {
		name     string
		response *eventbridge.DescribeArchiveOutput
		err      error
	}{
		{"nil", nil, nil},
		{"missing", nil, fmt.Errorf("wrapped: %w", &eventtypes.ResourceNotFoundException{})},
		{"other", nil, failure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &archiveFake{described: tt.response, readErr: tt.err}
			_, err := readArchive(t.Context(), f, "test")
			if tt.name == "other" {
				require.ErrorIs(t, err, failure)
				require.ErrorContains(t, err, "describe archive test")
			} else {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			}
			f.deleteErr = tt.err
			err = deleteArchive(t.Context(), f, "test")
			if tt.name == "other" {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
		})
	}
	r := ArchiveResource{Name: "test"}
	f := &archiveFake{createErr: failure}
	_, err := r.createArchive(t.Context(), f)
	require.ErrorIs(t, err, failure)
	assert.Equal(t, []any{&eventbridge.CreateArchiveInput{
		ArchiveName: aws.String("test"), EventSourceArn: aws.String("")}}, f.calls)
	f = &archiveFake{updateErr: failure}
	_, err = r.updateArchive(t.Context(), f,
		runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg]{})
	require.ErrorIs(t, err, failure)
	assert.Equal(t, []any{&eventbridge.UpdateArchiveInput{ArchiveName: aws.String("test")}}, f.calls)
}

func TestArchiveAcceptedCreateCleanup(t *testing.T) {
	for _, cleanupErr := range []error{nil, errors.New("cleanup failed"),
		&eventtypes.ResourceNotFoundException{}} {
		ctx, cancel := context.WithCancel(t.Context())
		failure := errors.New("read failed")
		f := &archiveFake{readErr: failure, deleteErr: cleanupErr,
			onRead: func(context.Context) { cancel() },
			onDelete: func(cleanup context.Context) {
				assert.NoError(t, cleanup.Err())
				deadline, ok := cleanup.Deadline()
				assert.True(t, ok)
				assert.WithinDuration(t, time.Now().Add(2*time.Minute), deadline, time.Second)
			}}
		r := ArchiveResource{Name: "accepted"}
		out, err := r.createArchive(ctx, f)
		cancel()
		assert.Nil(t, out)
		require.ErrorIs(t, err, failure)
		if cleanupErr != nil && !isNotFound(cleanupErr) {
			require.ErrorIs(t, err, cleanupErr)
		}
		assert.Equal(t, []any{&eventbridge.CreateArchiveInput{ArchiveName: aws.String("accepted"),
			EventSourceArn: aws.String("")},
			&eventbridge.DescribeArchiveInput{ArchiveName: aws.String("accepted")},
			&eventbridge.DeleteArchiveInput{ArchiveName: aws.String("accepted")}}, f.calls)
	}
}

func TestArchiveConfiguredRetries(t *testing.T) {
	for _, tt := range []struct {
		name, code, message string
		attempts            int
		cancel              bool
	}{
		{"transient", "InternalException", "retry", 2, false},
		{"quota", "LimitExceededException",
			"The requested resource exceeds the maximum number allowed", 1, false},
		{"other limit", "LimitExceededException", "Rate exceeded", 2, false},
		{"cancelled", "InternalException", "retry", 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var operations []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				operations = append(operations, r.Header.Get("X-Amz-Target"))
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				if len(operations) == 1 {
					if tt.cancel {
						cancel()
					}
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprintf(w, `{"__type":%q,"message":%q}`, tt.code, tt.message)
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			client := newClientFromConfig(aws.Config{
				Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
				Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
				}),
				Retryer: func() aws.Retryer { return eventbridgeStubRetryer{retryable: true} },
			})
			_, err := client.CreateArchive(ctx, &eventbridge.CreateArchiveInput{
				ArchiveName: aws.String("test"), EventSourceArn: aws.String(archiveSource)})
			if tt.attempts == 2 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if tt.cancel {
				require.ErrorIs(t, err, context.Canceled)
			}
			want := []string{"AWSEvents.CreateArchive"}
			if tt.attempts == 2 {
				want = append(want, "AWSEvents.CreateArchive")
			}
			assert.Equal(t, want, operations)
		})
	}
}

func TestArchiveManagedObservations(t *testing.T) {
	in := &eventbridge.CreateArchiveInput{
		Description: aws.String("description"), EventPattern: aws.String(`{"x":1}`),
		KmsKeyIdentifier: aws.String("key"), RetentionDays: aws.Int32(30)}
	base := ArchiveResourceOutput{Description: "description", EventPattern: `{ "x": 1.0 }`,
		KmsKeyIdentifier: "key", RetentionDays: 30}
	assert.False(t, archiveNeedsUpdate(in, &base))
	for _, mutate := range []func(*ArchiveResourceOutput){
		func(out *ArchiveResourceOutput) { out.Description = "drift" },
		func(out *ArchiveResourceOutput) { out.EventPattern = `{}` },
		func(out *ArchiveResourceOutput) { out.KmsKeyIdentifier = "drift" },
		func(out *ArchiveResourceOutput) { out.RetentionDays = 7 },
	} {
		out := base
		mutate(&out)
		assert.True(t, archiveNeedsUpdate(in, &out))
		assert.False(t, archiveNeedsUpdate(&eventbridge.CreateArchiveInput{}, &out))
	}
}

func TestArchiveCreateRefreshAbsence(t *testing.T) {
	for _, readErr := range []error{nil, &eventtypes.ResourceNotFoundException{}} {
		f := &archiveFake{readErr: readErr}
		r := ArchiveResource{Name: "accepted"}
		out, err := r.createArchive(t.Context(), f)
		assert.Nil(t, out)
		require.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Equal(t, []any{&eventbridge.CreateArchiveInput{
			ArchiveName: aws.String("accepted"), EventSourceArn: aws.String("")},
			&eventbridge.DescribeArchiveInput{ArchiveName: aws.String("accepted")},
			&eventbridge.DeleteArchiveInput{ArchiveName: aws.String("accepted")}}, f.calls)
	}
}
