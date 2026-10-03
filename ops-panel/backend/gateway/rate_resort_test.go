package gateway

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

type fakeChannelAPIForResort struct {
	groups map[uint][]connector.APIKeyGroup
}

func (f *fakeChannelAPIForResort) ListAPIKeys(ctx context.Context, channelID uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	return &connector.APIKeyPage{}, nil
}

func (f *fakeChannelAPIForResort) ListAPIKeyGroups(ctx context.Context, channelID uint) ([]connector.APIKeyGroup, error) {
	return f.groups[channelID], nil
}

func (f *fakeChannelAPIForResort) CreateAPIKey(ctx context.Context, channelID uint, req connector.APIKeyCreateRequest) (*connector.APIKey, error) {
	return nil, nil
}

func (f *fakeChannelAPIForResort) UpdateAPIKey(ctx context.Context, channelID uint, keyID int64, req connector.APIKeyUpdateRequest) (*connector.APIKey, error) {
	return nil, nil
}

func (f *fakeChannelAPIForResort) RevealAPIKey(ctx context.Context, channelID uint, keyID int64) (string, error) {
	return "", nil
}

func openGatewayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(storage.DBConfig{
		Driver: storage.DBDriverSQLite,
		Path:   filepath.Join(t.TempDir(), "gateway-test.db"),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestResortRoutesOnRateScan_OnlyEnabledGroups(t *testing.T) {
	db := openGatewayTestDB(t)
	groupsRepo := storage.NewGatewayGroups(db)
	routesRepo := storage.NewGatewayRoutes(db)

	gOn := &storage.GatewayGroup{
		Name:              "resort-on",
		Status:            storage.GatewayGroupStatusActive,
		RateSortDirection: "asc",
		RateResortEnabled: true,
	}
	gOff := &storage.GatewayGroup{
		Name:              "resort-off",
		Status:            storage.GatewayGroupStatusActive,
		RateSortDirection: "asc",
		RateResortEnabled: false,
	}
	if err := groupsRepo.Create(gOn); err != nil {
		t.Fatalf("create on: %v", err)
	}
	if err := groupsRepo.Create(gOff); err != nil {
		t.Fatalf("create off: %v", err)
	}

	gid1, gid2 := int64(11), int64(22)
	// 初始顺序：高倍率在前（与 asc 相反）
	onRoutes := []storage.GatewayRoute{
		{
			SourceKind: storage.GatewayRouteSourceMonitor, SourceChannelID: 1,
			SourceGroupID: &gid1, SourceGroupName: "high", Position: 0, Weight: 1,
			Enabled: true, RateConvertMode: "raw", BillingRateMultiplier: 0.3,
			SourceAPIKeyCipher: "k1",
		},
		{
			SourceKind: storage.GatewayRouteSourceMonitor, SourceChannelID: 1,
			SourceGroupID: &gid2, SourceGroupName: "low", Position: 1, Weight: 1,
			Enabled: true, RateConvertMode: "raw", BillingRateMultiplier: 0.1,
			SourceAPIKeyCipher: "k2",
		},
	}
	offRoutes := []storage.GatewayRoute{
		{
			SourceKind: storage.GatewayRouteSourceMonitor, SourceChannelID: 1,
			SourceGroupID: &gid1, SourceGroupName: "high", Position: 0, Weight: 1,
			Enabled: true, RateConvertMode: "raw", BillingRateMultiplier: 0.3,
			SourceAPIKeyCipher: "k1",
		},
		{
			SourceKind: storage.GatewayRouteSourceMonitor, SourceChannelID: 1,
			SourceGroupID: &gid2, SourceGroupName: "low", Position: 1, Weight: 1,
			Enabled: true, RateConvertMode: "raw", BillingRateMultiplier: 0.1,
			SourceAPIKeyCipher: "k2",
		},
	}
	if err := routesRepo.SaveForGroup(gOn.ID, onRoutes); err != nil {
		t.Fatalf("save on routes: %v", err)
	}
	if err := routesRepo.SaveForGroup(gOff.ID, offRoutes); err != nil {
		t.Fatalf("save off routes: %v", err)
	}

	fake := &fakeChannelAPIForResort{
		groups: map[uint][]connector.APIKeyGroup{
			1: {
				{ID: &gid1, Name: "high", Ratio: 0.3},
				{ID: &gid2, Name: "low", Ratio: 0.1},
			},
		},
	}
	svc := NewService(groupsRepo, storage.NewGatewayKeys(db), routesRepo, nil, nil, nil, fake, nil, nil)

	svc.ResortRoutesOnRateScan(context.Background())

	onList, err := routesRepo.ListByGroupID(gOn.ID)
	if err != nil {
		t.Fatalf("list on: %v", err)
	}
	if len(onList) != 2 {
		t.Fatalf("on routes len=%d", len(onList))
	}
	// asc：0.1 应在前
	if onList[0].SourceGroupName != "low" || onList[1].SourceGroupName != "high" {
		t.Fatalf("enabled group not resorteded: %+v / %+v", onList[0], onList[1])
	}
	if onList[0].BillingRateMultiplier != 0.1 || onList[1].BillingRateMultiplier != 0.3 {
		t.Fatalf("billing rates not refreshed: %v / %v", onList[0].BillingRateMultiplier, onList[1].BillingRateMultiplier)
	}

	offList, err := routesRepo.ListByGroupID(gOff.ID)
	if err != nil {
		t.Fatalf("list off: %v", err)
	}
	if offList[0].SourceGroupName != "high" || offList[1].SourceGroupName != "low" {
		t.Fatalf("disabled resort group should keep order: %+v / %+v", offList[0], offList[1])
	}
}

func TestUpdateGroup_RateResortTurnedOnReorders(t *testing.T) {
	db := openGatewayTestDB(t)
	groupsRepo := storage.NewGatewayGroups(db)
	routesRepo := storage.NewGatewayRoutes(db)

	g := &storage.GatewayGroup{
		Name:              "toggle-on",
		Status:            storage.GatewayGroupStatusActive,
		RateSortDirection: "asc",
		RateResortEnabled: false,
	}
	if err := groupsRepo.Create(g); err != nil {
		t.Fatalf("create: %v", err)
	}
	gidA, gidB := int64(1), int64(2)
	if err := routesRepo.SaveForGroup(g.ID, []storage.GatewayRoute{
		{
			SourceChannelID: 1, SourceGroupID: &gidA, SourceGroupName: "a",
			Position: 0, Weight: 1, Enabled: true, RateConvertMode: "raw",
			BillingRateMultiplier: 0.5, SourceAPIKeyCipher: "x",
		},
		{
			SourceChannelID: 1, SourceGroupID: &gidB, SourceGroupName: "b",
			Position: 1, Weight: 1, Enabled: true, RateConvertMode: "raw",
			BillingRateMultiplier: 0.2, SourceAPIKeyCipher: "y",
		},
	}); err != nil {
		t.Fatalf("save routes: %v", err)
	}

	fake := &fakeChannelAPIForResort{
		groups: map[uint][]connector.APIKeyGroup{
			1: {
				{ID: &gidA, Name: "a", Ratio: 0.5},
				{ID: &gidB, Name: "b", Ratio: 0.2},
			},
		},
	}
	svc := NewService(groupsRepo, storage.NewGatewayKeys(db), routesRepo, nil, nil, nil, fake, nil, nil)

	on := true
	if _, err := svc.UpdateGroup(g.ID, UpdateGroupInput{RateResortEnabled: &on}); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := routesRepo.ListByGroupID(g.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].SourceGroupName != "b" || list[1].SourceGroupName != "a" {
		t.Fatalf("turn-on should reorder: %+v / %+v", list[0], list[1])
	}
}
