//go:build unit

package payment

import "testing"

func TestNormalizeEPUSDTNetworkMapsGMPayAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "bsc", want: "bsc"},
		{in: "BSC", want: "bsc"},
		{in: "binance", want: "bsc"},
		{in: "bep20", want: "bsc"},
		{in: "bnb", want: "bsc"},
		{in: "tron", want: "trc20"},
		{in: "trc20", want: "trc20"},
		{in: "ethereum", want: "erc20"},
		{in: "erc20", want: "erc20"},
		{in: "polygon", want: "polygon"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got := NormalizeEPUSDTNetwork(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizeEPUSDTNetwork(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEPUSDTUpstreamNetworkUsesGMPayChainIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "bsc", want: "binance"},
		{in: "binance", want: "binance"},
		{in: "bep20", want: "binance"},
		{in: "trc20", want: "tron"},
		{in: "tron", want: "tron"},
		{in: "erc20", want: "ethereum"},
		{in: "ethereum", want: "ethereum"},
		{in: "polygon", want: "polygon"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got := EPUSDTUpstreamNetwork(tt.in)
			if got != tt.want {
				t.Fatalf("EPUSDTUpstreamNetwork(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEPUSDTNetworkFromMapAcceptsGMPayChainIDs(t *testing.T) {
	t.Parallel()

	got := EPUSDTNetworkFromMap(map[string]string{"network": "binance"})
	if got != "bsc" {
		t.Fatalf("EPUSDTNetworkFromMap(binance) = %q, want bsc", got)
	}
}

func TestEPUSDTNetworksFromMapParsesMultipleAndAliases(t *testing.T) {
	t.Parallel()

	got := EPUSDTNetworksFromMap(map[string]string{
		"networks": "binance, tron, ethereum",
	})
	want := []string{"bsc", "trc20", "erc20"}
	if len(got) != len(want) {
		t.Fatalf("EPUSDTNetworksFromMap() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("EPUSDTNetworksFromMap() = %v, want %v", got, want)
		}
	}
}

func TestEPUSDTNetworksFromMapFallsBackToSingleNetwork(t *testing.T) {
	t.Parallel()

	got := EPUSDTNetworksFromMap(map[string]string{"network": "bsc"})
	if len(got) != 1 || got[0] != "bsc" {
		t.Fatalf("EPUSDTNetworksFromMap(network=bsc) = %v, want [bsc]", got)
	}
}

func TestEPUSDTInstanceSupportsNetwork(t *testing.T) {
	t.Parallel()

	cfg := map[string]string{"networks": "bsc,trc20"}
	if !EPUSDTInstanceSupportsNetwork(cfg, "trc20") {
		t.Fatal("expected trc20 to be supported")
	}
	if EPUSDTInstanceSupportsNetwork(cfg, "polygon") {
		t.Fatal("did not expect polygon to be supported")
	}
}
