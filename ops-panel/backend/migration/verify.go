package migration

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

type expectedImport struct {
	legacyTable    string
	legacyID       string
	canonicalTable string
	canonicalID    string
	source         any
}

func (r *Runner) verifySnapshot(db *gorm.DB, snapshot *legacySnapshot) (map[string]int, error) {
	expected := expectedImports(snapshot)
	active, err := ActiveImports(db, r.version)
	if err != nil {
		return nil, err
	}
	if len(active) != len(expected) {
		return nil, fmt.Errorf("verify import ledger: got %d active rows, want %d", len(active), len(expected))
	}

	activeByKey := make(map[string]LegacyImportMap, len(active))
	for _, row := range active {
		key := importIdentity(row.LegacyTable, row.LegacyID, row.CanonicalTable)
		if _, exists := activeByKey[key]; exists {
			return nil, fmt.Errorf("verify import ledger: duplicate mapping %s", key)
		}
		activeByKey[key] = row
	}

	counts := make(map[string]int)
	for _, item := range expected {
		key := importIdentity(item.legacyTable, item.legacyID, item.canonicalTable)
		row, ok := activeByKey[key]
		if !ok {
			return nil, fmt.Errorf("verify import ledger: missing mapping %s", key)
		}
		fingerprint, err := Fingerprint(item.source)
		if err != nil {
			return nil, err
		}
		if row.CanonicalID != item.canonicalID {
			return nil, fmt.Errorf("%w: %s maps to %s, want %s", ErrCanonicalMappingChanged, key, row.CanonicalID, item.canonicalID)
		}
		if row.SourceFingerprint != fingerprint {
			return nil, fmt.Errorf("%w: %s", ErrSourceFingerprintChanged, key)
		}
		counts[item.canonicalTable]++
		delete(activeByKey, key)
	}
	if len(activeByKey) != 0 {
		return nil, fmt.Errorf("verify import ledger: %d unexpected active mappings", len(activeByKey))
	}

	if err := r.verifyTargets(db, snapshot); err != nil {
		return nil, err
	}
	if err := r.verifyChannels(db, snapshot); err != nil {
		return nil, err
	}
	if err := r.verifyRates(db, snapshot); err != nil {
		return nil, err
	}
	if err := r.verifyRateChanges(db, snapshot); err != nil {
		return nil, err
	}
	if err := r.verifyGroupBindings(db, snapshot); err != nil {
		return nil, err
	}
	return counts, nil
}

func expectedImports(snapshot *legacySnapshot) []expectedImport {
	items := make([]expectedImport, 0)
	for _, source := range snapshot.Connections {
		items = append(items, newExpectedImport("connections", source.ID, "upstream_sync_targets", source.ID, source))
	}
	for _, source := range snapshot.Sites {
		items = append(items, newExpectedImport("bl_collection_sites", source.ID, "channels", source.ID, source))
		if legacySiteHasSession(source) {
			items = append(items, newExpectedImport("bl_collection_sites", source.ID, "auth_sessions", source.ID, source))
		}
	}
	for _, source := range snapshot.Rates {
		if _, ok := effectiveLegacyRate(source); ok {
			items = append(items, newExpectedImport("bl_collected_group_rates", source.ID, "rate_snapshots", source.ID, source))
		}
	}
	for _, source := range snapshot.Changes {
		site, ok := legacySiteByID(snapshot, source.SiteID)
		if ok {
			if _, valid := parseLegacyRatio(source.NewValue, site.RechargeRatio); valid {
				items = append(items, newExpectedImport("bl_collected_changes", source.ID, "rate_change_logs", source.ID, source))
			}
		}
	}
	groups := groupedLegacyBindings(snapshot)
	for _, key := range sortedBindingKeys(groups) {
		bindings := groups[key]
		first := bindings[0]
		items = append(items, newExpectedImport("bl_source_bindings", first.ID, "upstream_sync_groups", first.ID, first))
		for _, source := range bindings {
			items = append(items, newExpectedImport("bl_source_bindings", source.ID, "upstream_sync_accounts", source.ID, source))
		}
	}
	return items
}

func newExpectedImport(legacyTable string, legacyID any, canonicalTable string, canonicalID any, source any) expectedImport {
	return expectedImport{
		legacyTable:    legacyTable,
		legacyID:       fmt.Sprint(legacyID),
		canonicalTable: canonicalTable,
		canonicalID:    fmt.Sprint(canonicalID),
		source:         source,
	}
}

func importIdentity(legacyTable, legacyID, canonicalTable string) string {
	return legacyTable + "[" + legacyID + "]->" + canonicalTable
}

func (r *Runner) verifyTargets(db *gorm.DB, snapshot *legacySnapshot) error {
	for _, source := range snapshot.Connections {
		var row storage.UpstreamSyncTarget
		if err := db.Where("id = ?", source.ID).Take(&row).Error; err != nil {
			return fmt.Errorf("verify upstream target %d: %w", source.ID, err)
		}
		plain, err := r.appCipher.Decrypt(row.AdminAPIKeyCipher)
		if err != nil {
			return fmt.Errorf("verify upstream target %d admin key: decrypt failed", source.ID)
		}
		expectedAPIKey, err := r.legacyCipher.Decrypt(source.AdminAPIKey)
		if err != nil {
			return fmt.Errorf("verify upstream target %d legacy admin key: decrypt failed", source.ID)
		}
		if row.ID != source.ID || row.Name != strings.TrimSpace(source.Name) ||
			row.BaseURL != strings.TrimRight(strings.TrimSpace(source.BaseURL), "/") ||
			row.Enabled != source.Enabled || plain != expectedAPIKey {
			return fmt.Errorf("verify upstream target %d: canonical data mismatch", source.ID)
		}
	}
	return nil
}

func (r *Runner) verifyChannels(db *gorm.DB, snapshot *legacySnapshot) error {
	for _, source := range snapshot.Sites {
		var row storage.Channel
		if err := db.Where("id = ?", source.ID).Take(&row).Error; err != nil {
			return fmt.Errorf("verify channel %d: %w", source.ID, err)
		}
		mode, expectedCredential, err := r.legacyChannelCredential(source)
		if err != nil {
			return err
		}
		credential, err := r.appCipher.Decrypt(row.PasswordCipher)
		if err != nil {
			return fmt.Errorf("verify channel %d credential: decrypt failed", source.ID)
		}
		typeName := strings.ToLower(strings.TrimSpace(source.SiteType))
		if typeName == "new_api" {
			typeName = string(storage.ChannelTypeNewAPI)
		}
		if row.ID != source.ID || row.Name != strings.TrimSpace(source.Name) || string(row.Type) != typeName ||
			row.SiteURL != strings.TrimRight(strings.TrimSpace(source.BaseURL), "/") ||
			row.Username != strings.TrimSpace(source.Email) || row.CredentialMode != mode ||
			credential != expectedCredential || row.MonitorEnabled != source.Enabled ||
			row.RechargeMultiplier == nil || !sameFloat(*row.RechargeMultiplier, source.RechargeRatio) ||
			row.RechargeMultiplierMode != "divide" || row.RateIntervalMinutes != source.IntervalMin ||
			!sameOptionalTime(row.LastRateScanAt, source.LastRunAt) {
			return fmt.Errorf("verify channel %d: canonical data mismatch", source.ID)
		}

		if !legacySiteHasSession(source) {
			continue
		}
		var session storage.AuthSession
		if err := db.Where("channel_id = ?", source.ID).Take(&session).Error; err != nil {
			return fmt.Errorf("verify channel %d session: %w", source.ID, err)
		}
		access, refresh, userID, cookie := splitLegacyTokens(source)
		actualAccess, err := r.appCipher.Decrypt(session.AccessTokenCipher)
		if err != nil {
			return fmt.Errorf("verify channel %d access token: decrypt failed", source.ID)
		}
		actualRefresh, err := r.appCipher.Decrypt(session.RefreshTokenCipher)
		if err != nil {
			return fmt.Errorf("verify channel %d refresh token: decrypt failed", source.ID)
		}
		actualCookie, err := r.appCipher.Decrypt(session.CookieCipher)
		if err != nil {
			return fmt.Errorf("verify channel %d cookie: decrypt failed", source.ID)
		}
		if session.ChannelID != source.ID || session.UserID != userID || actualAccess != access ||
			actualRefresh != refresh || actualCookie != cookie ||
			!sameOptionalTime(session.ExpiresAt, legacyTokenExpiry(source.TokenExpire)) {
			return fmt.Errorf("verify channel %d session: canonical data mismatch", source.ID)
		}
	}
	return nil
}

func (r *Runner) verifyRates(db *gorm.DB, snapshot *legacySnapshot) error {
	names := canonicalRateNames(snapshot.Rates)
	for _, source := range snapshot.Rates {
		value, ok := effectiveLegacyRate(source)
		if !ok {
			continue
		}
		site, _ := legacySiteByID(snapshot, source.SiteID)
		var row storage.RateSnapshot
		if err := db.Where("id = ?", source.ID).Take(&row).Error; err != nil {
			return fmt.Errorf("verify rate snapshot %d: %w", source.ID, err)
		}
		remoteID, hasRemoteID := parseInt64(source.GroupID)
		remoteMatches := (!hasRemoteID && row.RemoteGroupID == nil) ||
			(hasRemoteID && row.RemoteGroupID != nil && *row.RemoteGroupID == remoteID)
		if row.ChannelID != source.SiteID || row.ModelName != names[source.ID] ||
			!sameFloat(row.Ratio, value/site.RechargeRatio) || !sameFloat(row.CompletionRatio, 0) || !remoteMatches {
			return fmt.Errorf("verify rate snapshot %d: canonical data mismatch", source.ID)
		}
	}
	return nil
}

func (r *Runner) verifyRateChanges(db *gorm.DB, snapshot *legacySnapshot) error {
	names := latestRateNamesBySource(snapshot)
	for _, source := range snapshot.Changes {
		site, _ := legacySiteByID(snapshot, source.SiteID)
		newRatio, ok := parseLegacyRatio(source.NewValue, site.RechargeRatio)
		if !ok {
			continue
		}
		var row storage.RateChangeLog
		if err := db.Where("id = ?", source.ID).Take(&row).Error; err != nil {
			return fmt.Errorf("verify rate change %d: %w", source.ID, err)
		}
		name := names[sourceRateKey(source.SiteID, source.EntityKey)]
		if name == "" {
			name = strings.TrimSpace(source.EntityKey)
		}
		oldRatio, hasOld := parseLegacyRatio(source.OldValue, site.RechargeRatio)
		oldMatches := (!hasOld && row.OldRatio == nil) || (hasOld && row.OldRatio != nil && sameFloat(*row.OldRatio, oldRatio))
		if row.ChannelID != source.SiteID || row.ModelName != name || !oldMatches ||
			!sameFloat(row.NewRatio, newRatio) || !sameFloat(row.NewCompletionRatio, 0) {
			return fmt.Errorf("verify rate change %d: canonical data mismatch", source.ID)
		}
	}
	return nil
}

func (r *Runner) verifyGroupBindings(db *gorm.DB, snapshot *legacySnapshot) error {
	groups := groupedLegacyBindings(snapshot)
	for _, key := range sortedBindingKeys(groups) {
		bindings := groups[key]
		first := bindings[0]
		var group storage.UpstreamSyncGroup
		if err := db.Where("id = ?", first.ID).Take(&group).Error; err != nil {
			return fmt.Errorf("verify sync group %d: %w", first.ID, err)
		}
		name := fmt.Sprintf("legacy-target-%d-group-%d", first.ConnectionID, first.TargetID)
		targetIDs, _ := json.Marshal([]int64{first.TargetID})
		if group.TargetID != first.ConnectionID || group.Name != name || group.NameTemplate != name ||
			group.TargetGroupIDsJSON != string(targetIDs) || group.Enabled ||
			group.ApplyStatus != "migrated_extension_owned" {
			return fmt.Errorf("verify sync group %d: canonical data mismatch", first.ID)
		}
		for position, source := range bindings {
			var account storage.UpstreamSyncAccount
			if err := db.Where("id = ?", source.ID).Take(&account).Error; err != nil {
				return fmt.Errorf("verify sync account %d: %w", source.ID, err)
			}
			site, _ := legacySiteByID(snapshot, source.SourceSiteID)
			sourceGroupID, hasSourceGroupID := parseInt64(source.SourceGroupID)
			sourceGroupMatches := (!hasSourceGroupID && account.SourceGroupID == nil) ||
				(hasSourceGroupID && account.SourceGroupID != nil && *account.SourceGroupID == sourceGroupID)
			if account.SyncGroupID != first.ID || account.Position != position ||
				account.SourceChannelID != source.SourceSiteID || !sourceGroupMatches ||
				account.RateConvertMode != "divide" || !sameFloat(account.RateConvertValue, site.RechargeRatio) ||
				account.Enabled || account.TestEnabled {
				return fmt.Errorf("verify sync account %d: canonical data mismatch", source.ID)
			}
		}
	}
	return nil
}

func groupedLegacyBindings(snapshot *legacySnapshot) map[string][]legacySourceBinding {
	groups := make(map[string][]legacySourceBinding)
	for _, binding := range snapshot.Bindings {
		if strings.EqualFold(strings.TrimSpace(binding.TargetType), "group") {
			key := bindingTargetKey(binding)
			groups[key] = append(groups[key], binding)
		}
	}
	for key := range groups {
		sort.Slice(groups[key], func(i, j int) bool {
			return groups[key][i].ID < groups[key][j].ID
		})
	}
	return groups
}

func sortedBindingKeys(groups map[string][]legacySourceBinding) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sameFloat(a, b float64) bool {
	delta := math.Abs(a - b)
	scale := math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	return delta <= scale*1e-9
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UTC().Equal(b.UTC())
}

func canonicalUint(value string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid canonical id %q", value)
	}
	return uint(parsed), nil
}
