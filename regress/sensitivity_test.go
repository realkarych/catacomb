package regress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMinFullFlipRuns(t *testing.T) {
	th := DefaultThresholds()
	require.Equal(t, 3, minFullFlipRuns(th, th.PresenceDelta))
	th.Z = 1.96
	require.Equal(t, 4, minFullFlipRuns(th, th.PresenceDelta))
	th.PresenceDelta = 1.0
	require.Equal(t, 0, minFullFlipRuns(th, th.PresenceDelta))
}

func TestSensitivityOmittedWhenReachable(t *testing.T) {
	rep := Compare(inputWithFullPresenceFlipK3(t), DefaultThresholds())
	require.Nil(t, rep.Sensitivity)
}

func TestSensitivityPresentWhenUnreachable(t *testing.T) {
	th := DefaultThresholds()
	th.Z = 1.96
	rep := Compare(inputWithFullPresenceFlipK3(t), th)
	require.NotNil(t, rep.Sensitivity)
	require.False(t, rep.Sensitivity.Presence.Reachable)
	require.Equal(t, 4, rep.Sensitivity.Presence.MinFullFlipRuns)
	require.False(t, rep.Sensitivity.ErrorRate.Reachable)
}
