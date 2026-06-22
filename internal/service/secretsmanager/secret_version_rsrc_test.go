package secretsmanager

import (
	"regexp"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretVersionStageDiff(t *testing.T) {
	tests := []struct {
		name       string
		current    []string
		desired    []string
		wantAdd    []string
		wantRemove []string
	}{
		{
			name:       "adds and removes as a set",
			current:    []string{"old", "keep"},
			desired:    []string{"new", "keep"},
			wantAdd:    []string{"new"},
			wantRemove: []string{"old"},
		},
		{
			name:       "normalizes duplicates and empty strings",
			current:    []string{"b", "", "a", "b"},
			desired:    []string{"a", "", "c", "c"},
			wantAdd:    []string{"c"},
			wantRemove: []string{"b"},
		},
		{
			name:       "empty desired removes current labels",
			current:    []string{"AWSPENDING"},
			desired:    nil,
			wantAdd:    []string{},
			wantRemove: []string{"AWSPENDING"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAdd, gotRemove := secretVersionStageDiff(tt.current, tt.desired)
			assert.Equal(t, tt.wantAdd, gotAdd)
			assert.Equal(t, tt.wantRemove, gotRemove)
		})
	}
}

func TestSecretVersionStageActionNeeded(t *testing.T) {
	tests := []struct {
		name    string
		current []string
		desired []string
		want    bool
	}{
		{
			name:    "explicit empty removes custom labels",
			current: []string{"AWSPENDING"},
			want:    true,
		},
		{
			name:    "only current has no removable change",
			current: []string{currentStage},
			want:    false,
		},
		{
			name:    "adds labels",
			current: []string{"old"},
			desired: []string{"new", "old"},
			want:    true,
		},
		{
			name:    "equal labels",
			current: []string{"old"},
			desired: []string{"old"},
			want:    false,
		},
		{
			name:    "empty labels never require stage writes",
			current: []string{""},
			desired: []string{""},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, secretVersionStageActionNeeded(tt.current, tt.desired))
		})
	}
}

func TestSecretVersionStagesNeedUpdate(t *testing.T) {
	tests := []struct {
		name     string
		current  *SecretVersion
		prior    runtime.Prior[SecretVersion, *SecretVersionOutput]
		wantNeed bool
	}{
		{
			name:    "omitted stages leave observed labels alone",
			current: &SecretVersion{},
			prior: runtime.Prior[SecretVersion, *SecretVersionOutput]{
				Observed: &SecretVersionOutput{VersionStages: []string{"AWSPENDING"}},
			},
		},
		{
			name:    "explicit empty changed from omitted reconciles labels",
			current: &SecretVersion{VersionStages: stringSlicePtr()},
			prior: runtime.Prior[SecretVersion, *SecretVersionOutput]{
				Observed: &SecretVersionOutput{VersionStages: []string{"AWSPENDING"}},
			},
			wantNeed: true,
		},
		{
			name:    "explicit empty removes observed custom label drift",
			current: &SecretVersion{VersionStages: stringSlicePtr()},
			prior: runtime.Prior[SecretVersion, *SecretVersionOutput]{
				Inputs:   SecretVersion{VersionStages: stringSlicePtr()},
				Observed: &SecretVersionOutput{VersionStages: []string{"AWSPENDING"}},
			},
			wantNeed: true,
		},
		{
			name:    "explicit empty ignores an unremovable current label",
			current: &SecretVersion{VersionStages: stringSlicePtr()},
			prior: runtime.Prior[SecretVersion, *SecretVersionOutput]{
				Inputs:   SecretVersion{VersionStages: stringSlicePtr()},
				Observed: &SecretVersionOutput{VersionStages: []string{currentStage}},
			},
		},
		{
			name:    "managed labels changing by input reconciles labels",
			current: &SecretVersion{VersionStages: stringSlicePtr("new")},
			prior: runtime.Prior[SecretVersion, *SecretVersionOutput]{
				Inputs:   SecretVersion{VersionStages: stringSlicePtr("old")},
				Observed: &SecretVersionOutput{VersionStages: []string{"old"}},
			},
			wantNeed: true,
		},
		{
			name:    "empty-only input changes do not reconcile labels",
			current: &SecretVersion{VersionStages: stringSlicePtr("old", "")},
			prior: runtime.Prior[SecretVersion, *SecretVersionOutput]{
				Inputs:   SecretVersion{VersionStages: stringSlicePtr("old")},
				Observed: &SecretVersionOutput{VersionStages: []string{"old"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantNeed, tt.current.stagesNeedUpdate(tt.prior))
		})
	}
}

func TestSecretVersionDecodeVersionStagesPresence(t *testing.T) {
	var omitted SecretVersion
	require.NoError(t, runtime.Decode(&omitted, map[string]any{"secret-id": "s"}))
	assert.Nil(t, omitted.VersionStages)

	var empty SecretVersion
	require.NoError(t, runtime.Decode(&empty, map[string]any{
		"secret-id":      "s",
		"version-stages": []any{},
	}))
	require.NotNil(t, empty.VersionStages)
	assert.Empty(t, *empty.VersionStages)
}

func stringSlicePtr(values ...string) *[]string {
	return &values
}

func TestSecretVersionDeleteComplete(t *testing.T) {
	tests := []struct {
		name   string
		stages []string
		want   bool
	}{
		{name: "no stages", stages: nil, want: true},
		{name: "empty stages are absent", stages: []string{""}, want: true},
		{name: "only current", stages: []string{currentStage}, want: true},
		{name: "only previous", stages: []string{previousStage}, want: true},
		{name: "custom stage remains", stages: []string{"AWSPENDING"}, want: false},
		{name: "more than one managed stage remains", stages: []string{currentStage, previousStage}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, secretVersionDeleteComplete(tt.stages))
		})
	}
}

func TestSecretVersionClientRequestToken(t *testing.T) {
	token, err := secretVersionClientRequestToken()
	require.NoError(t, err)
	assert.Regexp(t,
		regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`),
		token)
}
