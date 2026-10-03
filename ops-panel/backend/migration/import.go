package migration

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/channel"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

func expectedImportCounts(snapshot *legacySnapshot) map[string]int {
	counts := map[string]int{
		"upstream_sync_targets": len(snapshot.Connections),
		"channels":              len(snapshot.Sites),
	}
	for _, site := range snapshot.Sites {
		if legacySiteHasSession(site) {
			counts["auth_sessions"]++
		}
	}
	for _, rate := range snapshot.Rates {
		if _, ok := effectiveLegacyRate(rate); ok {
			counts["rate_snapshots"]++
		}
	}
	for _, change := range snapshot.Changes {
		site, ok := legacySiteByID(snapshot, change.SiteID)
		if ok {
			if _, valid := parseLegacyRatio(change.NewValue, site.RechargeRatio); valid {
				counts["rate_change_logs"]++
			}
		}
	}
	groupKeys := make(map[string]struct{})
	for _, binding := range snapshot.Bindings {
		if strings.EqualFold(strings.TrimSpace(binding.TargetType), "group") {
			groupKeys[bindingTargetKey(binding)] = struct{}{}
			counts["upstream_sync_accounts"]++
		}
	}
	counts["upstream_sync_groups"] = len(groupKeys)
	return counts
}

func expectedSkippedCounts(snapshot *legacySnapshot) map[string]int {
	skipped := map[string]int{}
	for _, rate := range snapshot.Rates {
		if _, ok := effectiveLegacyRate(rate); !ok {
			skipped["rate_snapshots_without_numeric_rate"]++
		}
	}
	for _, change := range snapshot.Changes {
		site, ok := legacySiteByID(snapshot, change.SiteID)
		if !ok {
			continue
		}
		if _, valid := parseLegacyRatio(change.NewValue, site.RechargeRatio); !valid {
			skipped["rate_changes_without_numeric_new_value"]++
		}
	}
	for _, binding := range snapshot.Bindings {
		if strings.EqualFold(strings.TrimSpace(binding.TargetType), "account") {
			skipped["account_rate_bindings_kept_in_extension"]++
		}
	}
	return skipped
}

func (r *Runner) importSnapshot(db *gorm.DB, snapshot *legacySnapshot) (map[string]int, error) {
	if err := storage.AutoMigrate(db); err != nil {
		return nil, fmt.Errorf("create upstream schema: %w", err)
	}
	counts := make(map[string]int)
	if err := r.importTargets(db, snapshot, counts); err != nil {
		return nil, err
	}
	if err := r.importChannels(db, snapshot, counts); err != nil {
		return nil, err
	}
	if err := r.importRates(db, snapshot, counts); err != nil {
		return nil, err
	}
	if err := r.importRateChanges(db, snapshot, counts); err != nil {
		return nil, err
	}
	if err := r.importGroupBindings(db, snapshot, counts); err != nil {
		return nil, err
	}
	if err := resetImportedSequences(db); err != nil {
		return nil, err
	}
	return counts, nil
}

func (r *Runner) importTargets(db *gorm.DB, snapshot *legacySnapshot, counts map[string]int) error {
	for _, source := range snapshot.Connections {
		plainAPIKey, err := r.legacyCipher.Decrypt(source.AdminAPIKey)
		if err != nil {
			return fmt.Errorf("decrypt legacy connection %d admin key: %w", source.ID, err)
		}
		ciphertext, err := r.appCipher.Encrypt(plainAPIKey)
		if err != nil {
			return fmt.Errorf("encrypt legacy connection %d admin key: %w", source.ID, err)
		}
		row := map[string]any{
			"id":                   source.ID,
			"name":                 strings.TrimSpace(source.Name),
			"base_url":             strings.TrimRight(strings.TrimSpace(source.BaseURL), "/"),
			"admin_api_key_cipher": ciphertext,
			"enabled":              source.Enabled,
			"last_check_at":        source.LastCheckAt,
			"created_at":           source.CreatedAt,
			"updated_at":           source.UpdatedAt,
		}
		if err := db.Table("upstream_sync_targets").Create(row).Error; err != nil {
			return fmt.Errorf("import legacy connection %d: %w", source.ID, err)
		}
		if err := r.recordMapping(db, "connections", source.ID, "upstream_sync_targets", source.ID, source); err != nil {
			return err
		}
		counts["upstream_sync_targets"]++
	}
	return nil
}

func (r *Runner) importChannels(db *gorm.DB, snapshot *legacySnapshot, counts map[string]int) error {
	for _, source := range snapshot.Sites {
		mode, rawCredential, err := r.legacyChannelCredential(source)
		if err != nil {
			return err
		}
		credentialCipher, err := r.appCipher.Encrypt(rawCredential)
		if err != nil {
			return fmt.Errorf("encrypt legacy collection site %d credential: %w", source.ID, err)
		}
		channelType := strings.ToLower(strings.TrimSpace(source.SiteType))
		if channelType == "new_api" {
			channelType = string(storage.ChannelTypeNewAPI)
		}
		row := map[string]any{
			"id":                       source.ID,
			"name":                     strings.TrimSpace(source.Name),
			"type":                     channelType,
			"site_url":                 strings.TrimRight(strings.TrimSpace(source.BaseURL), "/"),
			"username":                 strings.TrimSpace(source.Email),
			"sort_order":               int(source.ID),
			"password_cipher":          credentialCipher,
			"credential_mode":          mode,
			"ignore_announcements":     false,
			"subscription_enabled":     false,
			"proxy_enabled":            false,
			"recharge_multiplier":      source.RechargeRatio,
			"recharge_multiplier_mode": "divide",
			"monitor_enabled":          source.Enabled,
			"rate_interval_minutes":    source.IntervalMin,
			"last_rate_scan_at":        source.LastRunAt,
			"last_error":               strings.TrimSpace(stringValue(source.LastError)),
			"created_at":               source.CreatedAt,
			"updated_at":               source.UpdatedAt,
		}
		if err := db.Table("channels").Create(row).Error; err != nil {
			return fmt.Errorf("import legacy collection site %d: %w", source.ID, err)
		}
		if err := r.recordMapping(db, "bl_collection_sites", source.ID, "channels", source.ID, source); err != nil {
			return err
		}
		counts["channels"]++

		session, err := r.legacyAuthSession(source)
		if err != nil {
			return err
		}
		if session == nil {
			continue
		}
		if err := db.Table("auth_sessions").Create(session).Error; err != nil {
			return fmt.Errorf("import legacy collection site %d session: %w", source.ID, err)
		}
		if err := r.recordMapping(db, "bl_collection_sites", source.ID, "auth_sessions", source.ID, source); err != nil {
			return err
		}
		counts["auth_sessions"]++
	}
	return nil
}

func (r *Runner) legacyChannelCredential(site legacyCollectionSite) (storage.CredentialMode, string, error) {
	authMode := strings.ToLower(strings.TrimSpace(site.AuthMode))
	if authMode == "password" {
		plain, err := r.legacyCipher.Decrypt(site.PasswordEnc)
		if err != nil {
			return "", "", fmt.Errorf("decrypt legacy collection site %d password: %w", site.ID, err)
		}
		return storage.CredentialModePassword, plain, nil
	}
	access, refresh, userID, cookie := splitLegacyTokens(site)
	var credential any
	if strings.EqualFold(strings.TrimSpace(site.SiteType), "new_api") {
		credential = channel.NewAPITokenCredential{Cookie: cookie, AccessToken: access, UserID: userID}
	} else {
		credential = channel.Sub2APITokenCredential{AccessToken: access, RefreshToken: refresh}
	}
	body, err := json.Marshal(credential)
	if err != nil {
		return "", "", fmt.Errorf("encode legacy collection site %d token credential: %w", site.ID, err)
	}
	return storage.CredentialModeToken, string(body), nil
}

func (r *Runner) legacyAuthSession(site legacyCollectionSite) (map[string]any, error) {
	if !legacySiteHasSession(site) {
		return nil, nil
	}
	access, refresh, userID, cookie := splitLegacyTokens(site)
	accessCipher, err := r.appCipher.Encrypt(access)
	if err != nil {
		return nil, fmt.Errorf("encrypt legacy collection site %d access token: %w", site.ID, err)
	}
	refreshCipher, err := r.appCipher.Encrypt(refresh)
	if err != nil {
		return nil, fmt.Errorf("encrypt legacy collection site %d refresh token: %w", site.ID, err)
	}
	cookieCipher, err := r.appCipher.Encrypt(cookie)
	if err != nil {
		return nil, fmt.Errorf("encrypt legacy collection site %d cookie: %w", site.ID, err)
	}
	return map[string]any{
		"channel_id":           site.ID,
		"user_id":              userID,
		"access_token_cipher":  accessCipher,
		"refresh_token_cipher": refreshCipher,
		"cookie_cipher":        cookieCipher,
		"expires_at":           legacyTokenExpiry(site.TokenExpire),
		"last_login_at":        site.LastSuccessAt,
		"updated_at":           site.UpdatedAt,
	}, nil
}

func splitLegacyTokens(site legacyCollectionSite) (access, refresh, userID, cookie string) {
	access = strings.TrimSpace(stringValue(site.AccessToken))
	refresh = strings.TrimSpace(stringValue(site.RefreshToken))
	userID = strings.TrimSpace(stringValue(site.NewAPIUserID))
	if strings.EqualFold(strings.TrimSpace(site.SiteType), "new_api") {
		if index := strings.Index(access, "::"); index >= 0 {
			if candidate := strings.TrimSpace(access[index+2:]); candidate != "" {
				userID = candidate
			}
			access = strings.TrimSpace(access[:index])
		}
		lower := strings.ToLower(access)
		if strings.HasPrefix(lower, "session:") {
			cookie = strings.TrimSpace(access[len("session:"):])
			access = ""
		} else if strings.HasPrefix(lower, "cookie:") {
			cookie = strings.TrimSpace(access[len("cookie:"):])
			access = ""
		} else if strings.HasPrefix(lower, "bearer ") {
			access = strings.TrimSpace(access[len("bearer "):])
		}
	}
	return access, refresh, userID, cookie
}

func legacySiteHasSession(site legacyCollectionSite) bool {
	access, refresh, _, cookie := splitLegacyTokens(site)
	return access != "" || refresh != "" || cookie != ""
}

func legacyTokenExpiry(raw *int64) *time.Time {
	if raw == nil || *raw <= 0 {
		return nil
	}
	seconds := *raw
	if seconds > 200_000_000_000 {
		seconds /= 1000
	}
	value := time.Unix(seconds, 0).UTC()
	return &value
}

func (r *Runner) importRates(db *gorm.DB, snapshot *legacySnapshot, counts map[string]int) error {
	names := canonicalRateNames(snapshot.Rates)
	for _, source := range snapshot.Rates {
		site, _ := legacySiteByID(snapshot, source.SiteID)
		ratio, ok := effectiveLegacyRate(source)
		if !ok {
			continue
		}
		ratio /= site.RechargeRatio
		remoteID, hasRemoteID := parseInt64(source.GroupID)
		row := map[string]any{
			"id":               source.ID,
			"channel_id":       source.SiteID,
			"model_name":       names[source.ID],
			"description":      "",
			"ratio":            ratio,
			"completion_ratio": 0,
			"first_seen_at":    source.CollectedAt,
			"last_seen_at":     source.CollectedAt,
		}
		if hasRemoteID {
			row["remote_group_id"] = remoteID
		}
		if err := db.Table("rate_snapshots").Create(row).Error; err != nil {
			return fmt.Errorf("import legacy rate %d: %w", source.ID, err)
		}
		if err := r.recordMapping(db, "bl_collected_group_rates", source.ID, "rate_snapshots", source.ID, source); err != nil {
			return err
		}
		counts["rate_snapshots"]++
	}
	return nil
}

func (r *Runner) importRateChanges(db *gorm.DB, snapshot *legacySnapshot, counts map[string]int) error {
	rateNames := latestRateNamesBySource(snapshot)
	for _, source := range snapshot.Changes {
		site, _ := legacySiteByID(snapshot, source.SiteID)
		newRatio, ok := parseLegacyRatio(source.NewValue, site.RechargeRatio)
		if !ok {
			continue
		}
		var oldRatio *float64
		if value, valid := parseLegacyRatio(source.OldValue, site.RechargeRatio); valid {
			oldRatio = &value
		}
		name := rateNames[sourceRateKey(source.SiteID, source.EntityKey)]
		if name == "" {
			name = strings.TrimSpace(source.EntityKey)
		}
		row := map[string]any{
			"id":                   source.ID,
			"channel_id":           source.SiteID,
			"model_name":           name,
			"old_ratio":            oldRatio,
			"new_ratio":            newRatio,
			"new_completion_ratio": 0,
			"changed_at":           source.CreatedAt,
		}
		if err := db.Table("rate_change_logs").Create(row).Error; err != nil {
			return fmt.Errorf("import legacy rate change %d: %w", source.ID, err)
		}
		if err := r.recordMapping(db, "bl_collected_changes", source.ID, "rate_change_logs", source.ID, source); err != nil {
			return err
		}
		counts["rate_change_logs"]++
	}
	return nil
}

func (r *Runner) importGroupBindings(db *gorm.DB, snapshot *legacySnapshot, counts map[string]int) error {
	groups := make(map[string][]legacySourceBinding)
	for _, binding := range snapshot.Bindings {
		if !strings.EqualFold(strings.TrimSpace(binding.TargetType), "group") {
			continue
		}
		key := bindingTargetKey(binding)
		groups[key] = append(groups[key], binding)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		bindings := groups[key]
		sort.Slice(bindings, func(i, j int) bool { return bindings[i].ID < bindings[j].ID })
		first := bindings[0]
		groupID := first.ID
		name := fmt.Sprintf("legacy-target-%d-group-%d", first.ConnectionID, first.TargetID)
		displayName := strings.TrimSpace(stringValue(first.SourceGroupName))
		if displayName == "" {
			displayName = name
		}
		platform := strings.TrimSpace(stringValue(first.SourcePlatform))
		if platform == "" {
			platform = "unknown"
		}
		targetIDs, _ := json.Marshal([]int64{first.TargetID})
		row := map[string]any{
			"id":                    groupID,
			"display_name":          displayName,
			"name_template":         name,
			"name":                  name,
			"target_id":             first.ConnectionID,
			"target_group_ids_json": string(targetIDs),
			"platform":              platform,
			"model_limits_mode":     "sync_upstream",
			"pool_mode_enabled":     false,
			"pool_mode_retry_count": 5,
			"rate_sort_direction":   "asc",
			"enabled":               false,
			"apply_status":          "migrated_extension_owned",
			"created_at":            first.CreatedAt,
			"updated_at":            first.UpdatedAt,
		}
		if err := db.Table("upstream_sync_groups").Create(row).Error; err != nil {
			return fmt.Errorf("import legacy binding group %s: %w", key, err)
		}
		if err := r.recordMapping(db, "bl_source_bindings", first.ID, "upstream_sync_groups", groupID, first); err != nil {
			return err
		}
		counts["upstream_sync_groups"]++

		for position, binding := range bindings {
			site, _ := legacySiteByID(snapshot, binding.SourceSiteID)
			sourceGroupID, hasSourceGroupID := parseInt64(binding.SourceGroupID)
			account := map[string]any{
				"id":                 binding.ID,
				"sync_group_id":      groupID,
				"position":           position,
				"source_channel_id":  binding.SourceSiteID,
				"source_group_name":  strings.TrimSpace(stringValue(binding.SourceGroupName)),
				"concurrency":        10,
				"weight":             1,
				"rate_convert_mode":  "divide",
				"rate_convert_value": site.RechargeRatio,
				"enabled":            false,
				"test_enabled":       false,
				"test_model":         "",
				"created_at":         binding.CreatedAt,
				"updated_at":         binding.UpdatedAt,
			}
			if hasSourceGroupID {
				account["source_group_id"] = sourceGroupID
			}
			if err := db.Table("upstream_sync_accounts").Create(account).Error; err != nil {
				return fmt.Errorf("import legacy source binding %d: %w", binding.ID, err)
			}
			if err := r.recordMapping(db, "bl_source_bindings", binding.ID, "upstream_sync_accounts", binding.ID, binding); err != nil {
				return err
			}
			counts["upstream_sync_accounts"]++
		}
	}
	return nil
}

func (r *Runner) recordMapping(db *gorm.DB, legacyTable string, legacyID any, canonicalTable string, canonicalID any, source any) error {
	fingerprint, err := Fingerprint(source)
	if err != nil {
		return err
	}
	canonicalFingerprint, err := canonicalRowFingerprint(db, canonicalTable, fmt.Sprint(canonicalID))
	if err != nil {
		return fmt.Errorf("fingerprint imported %s[%v]: %w", canonicalTable, canonicalID, err)
	}
	_, _, err = RecordImport(db, ImportRecord{
		MigrationVersion:     r.version,
		LegacyTable:          legacyTable,
		LegacyID:             fmt.Sprint(legacyID),
		CanonicalTable:       canonicalTable,
		CanonicalID:          fmt.Sprint(canonicalID),
		SourceFingerprint:    fingerprint,
		CanonicalFingerprint: canonicalFingerprint,
		ImportedAt:           r.now(),
	})
	return err
}

func canonicalRateNames(rates []legacyGroupRate) map[uint]string {
	baseCounts := make(map[string]int)
	bases := make(map[uint]string, len(rates))
	for _, rate := range rates {
		base := strings.TrimSpace(rate.Name)
		if base == "" {
			base = strings.TrimSpace(rate.GroupID)
		}
		bases[rate.ID] = base
		baseCounts[sourceRateKey(rate.SiteID, strings.ToLower(base))]++
	}
	out := make(map[uint]string, len(rates))
	for _, rate := range rates {
		base := bases[rate.ID]
		if baseCounts[sourceRateKey(rate.SiteID, strings.ToLower(base))] > 1 {
			base += " [" + strings.TrimSpace(rate.GroupID) + "]"
		}
		out[rate.ID] = base
	}
	return out
}

func latestRateNamesBySource(snapshot *legacySnapshot) map[string]string {
	names := canonicalRateNames(snapshot.Rates)
	out := make(map[string]string, len(snapshot.Rates))
	for _, rate := range snapshot.Rates {
		out[sourceRateKey(rate.SiteID, rate.GroupID)] = names[rate.ID]
	}
	return out
}

func legacySiteByID(snapshot *legacySnapshot, id uint) (legacyCollectionSite, bool) {
	for _, site := range snapshot.Sites {
		if site.ID == id {
			return site, true
		}
	}
	return legacyCollectionSite{}, false
}

func bindingTargetKey(binding legacySourceBinding) string {
	return fmt.Sprintf("%d:%s:%d", binding.ConnectionID, strings.ToLower(strings.TrimSpace(binding.TargetType)), binding.TargetID)
}

func parseInt64(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed, err == nil && parsed > 0
}

func resetImportedSequences(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	for _, table := range []string{
		"channels",
		"rate_snapshots",
		"rate_change_logs",
		"upstream_sync_targets",
		"upstream_sync_groups",
		"upstream_sync_accounts",
	} {
		query := fmt.Sprintf(
			"SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE(MAX(id), 1), COUNT(*) > 0) FROM %s",
			table, table,
		)
		if err := db.Exec(query).Error; err != nil {
			return fmt.Errorf("reset %s sequence: %w", table, err)
		}
	}
	return nil
}
