// 数据面：源分组列表缓存。
package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
)

func (rt *Runtime) loadGroupsByChannel(ctx context.Context, routes []storage.GatewayRoute) map[uint][]connector.APIKeyGroup {
	out := make(map[uint][]connector.APIKeyGroup)
	if rt.ChannelAPI == nil {
		return out
	}
	ids := make([]uint, 0)
	seen := make(map[uint]struct{})
	for _, r := range routes {
		if r.NormalizeSourceKind() == storage.GatewayRouteSourceProvider {
			continue
		}
		if r.SourceChannelID == 0 {
			continue
		}
		if _, ok := seen[r.SourceChannelID]; ok {
			continue
		}
		seen[r.SourceChannelID] = struct{}{}
		ids = append(ids, r.SourceChannelID)
	}
	if len(ids) == 0 {
		return out
	}

	// 先吃缓存，避免保存路由 / 运行时选路重复打上游。
	ttl := rt.gatewayRuntime().ModelsCacheTTL()
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	miss := make([]uint, 0, len(ids))
	now := time.Now()
	rt.channelGroupsCacheMu.Lock()
	if rt.channelGroupsCache == nil {
		rt.channelGroupsCache = map[uint]channelGroupsCacheEntry{}
	}
	for _, id := range ids {
		if ent, ok := rt.channelGroupsCache[id]; ok && now.Sub(ent.at) < ttl {
			out[id] = ent.groups
			continue
		}
		miss = append(miss, id)
	}
	rt.channelGroupsCacheMu.Unlock()

	if len(miss) == 0 {
		return out
	}

	fetchOne := func(id uint) []connector.APIKeyGroup {
		groups, err := rt.ChannelAPI.ListAPIKeyGroups(ctx, id)
		if err != nil {
			return nil
		}
		return groups
	}

	if len(miss) == 1 {
		groups := fetchOne(miss[0])
		out[miss[0]] = groups
		rt.storeChannelGroupsCache(miss[0], groups)
		return out
	}

	// 保存路由 / 运行时倍率排序：按渠道并发拉源分组，缩短批量等待。
	sem := make(chan struct{}, rt.gatewayRuntime().RouteBatchConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range miss {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				out[id] = nil
				mu.Unlock()
				return
			}
			groups := fetchOne(id)
			mu.Lock()
			out[id] = groups
			mu.Unlock()
			rt.storeChannelGroupsCache(id, groups)
		}()
	}
	wg.Wait()
	return out
}

func (rt *Runtime) storeChannelGroupsCache(channelID uint, groups []connector.APIKeyGroup) {
	rt.channelGroupsCacheMu.Lock()
	defer rt.channelGroupsCacheMu.Unlock()
	if rt.channelGroupsCache == nil {
		rt.channelGroupsCache = map[uint]channelGroupsCacheEntry{}
	}
	rt.channelGroupsCache[channelID] = channelGroupsCacheEntry{at: time.Now(), groups: groups}
}

// InvalidateChannelGroupsCache 清空源分组缓存（倍率扫描后调用，保证下次重排拿到新 ratio）。

func (rt *Runtime) InvalidateChannelGroupsCache() {
	rt.channelGroupsCacheMu.Lock()
	defer rt.channelGroupsCacheMu.Unlock()
	rt.channelGroupsCache = map[uint]channelGroupsCacheEntry{}
}

// upstreamTarget 解析后的上游目标（监控渠道或直连 Provider）。
