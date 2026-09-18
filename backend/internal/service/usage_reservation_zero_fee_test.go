package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageReservationReserveCommandPreservesExplicitZeroFee(t *testing.T) {
	cmd := &UsageReservationReserveCommand{
		ManagementFeeBPS:    0,
		ManagementFeeBPSSet: true,
	}

	cmd.Normalize()

	require.Zero(t, cmd.ManagementFeeBPS)
}
