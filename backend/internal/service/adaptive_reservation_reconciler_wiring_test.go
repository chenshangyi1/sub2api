package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderSetStartsAdaptiveReservationReconciler(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("wire.go")
	require.NoError(t, err)
	require.Contains(t, string(source), "ProvideAdaptiveReservationReconciler,",
		"expired in_flight adaptive holds stay frozen unless the reconciler is started by Wire")
}

func TestRepositoryProviderSetRegistersAdaptiveReconciliationEvidence(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "repository", "wire.go"))
	require.NoError(t, err)
	require.Contains(t, string(source), "NewAdaptiveReconciliationEvidenceRepository,",
		"the reconciler cannot inspect pending usage/attempts without the evidence repository in Wire")
}

func TestServerCleanupStopsAdaptiveReservationReconciler(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	cmdDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "cmd", "server")

	wireSource, err := os.ReadFile(filepath.Join(cmdDir, "wire.go"))
	require.NoError(t, err)
	require.Contains(t, string(wireSource), "adaptiveReservationReconciler *service.AdaptiveReservationReconciler")
	require.Contains(t, string(wireSource), `{"AdaptiveReservationReconciler"`)
	require.Contains(t, string(wireSource), "adaptiveReservationReconciler.Stop()")

	generated, err := os.ReadFile(filepath.Join(cmdDir, "wire_gen.go"))
	require.NoError(t, err)
	generatedText := string(generated)
	require.Contains(t, generatedText, "service.ProvideAdaptiveReservationReconciler(")
	require.Contains(t, generatedText, "repository.NewAdaptiveReconciliationEvidenceRepository(")
	require.True(t, strings.Contains(generatedText, "adaptiveReservationReconciler"),
		"generated Wire graph must keep the reconciler alive so provideCleanup can stop it")
}

func TestGeneratedWireGraphWiresModelPlazaAccountAndAdaptiveFallbacks(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	generated, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "cmd", "server", "wire_gen.go"))
	require.NoError(t, err)
	generatedText := string(generated)
	require.Contains(t, generatedText, "service.ProvideModelPlazaService(")
	require.Contains(t, generatedText, "adaptivePoolSnapshotRepository, accountRepository")
	require.Contains(t, generatedText, "service.ProvideAPIKeyService(")
	require.Contains(t, generatedText, "deferredService, adaptivePoolSnapshotRepository)")
}
