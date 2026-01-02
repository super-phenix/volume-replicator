package replicator

import (
	"github.com/skalanetworks/volume-replicator/internal/constants"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIsParentLabelPresent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		result bool
	}{
		{
			name: "not present",
			labels: map[string]string{
				"a": "b",
			},
			result: false,
		},
		{
			name: "present",
			labels: map[string]string{
				"a":                     "b",
				"c":                     "d",
				constants.VrParentLabel: "test",
			},
			result: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isParentLabelPresent(tt.labels)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestGetLabelsWithParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent string
		labels map[string]string
		result map[string]string
	}{
		{
			name:   "empty labels",
			parent: "test",
			labels: map[string]string{},
			result: map[string]string{
				constants.VrParentLabel: "test",
			},
		},
		{
			name:   "some labels",
			parent: "test",
			labels: map[string]string{
				"a": "b",
				"c": "d",
			},
			result: map[string]string{
				constants.VrParentLabel: "test",
				"a":                     "b",
				"c":                     "d",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLabelsWithParent(tt.labels, tt.parent)
			require.Equal(t, tt.result, result)
		})
	}
}
