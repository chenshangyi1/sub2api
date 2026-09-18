package service

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const groupMonitorDefaultMaxWorkers = 5

const (
	groupMonitorRunnerLeaderLockKey = "group_monitor:runner:leader"
	groupMonitorRunnerLeaderLockTTL = 6 * time.Minute
)

// groupMonitorHistoryRetention 历史记录保留时长（数据治理，防表膨胀）。
const groupMonitorHistoryRetention = 7 * 24 * time.Hour

// groupMonitorPruneInterval 历史清理执行间隔。
const groupMonitorPruneInterval = time.Hour

// GroupMonitorRunnerService periodically scans due group monitors and executes them.
type GroupMonitorRunnerService struct {
	svc        *GroupMonitorService
	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once

	pruneMu   sync.Mutex
	lastPrune time.Time
}

// NewGroupMonitorRunnerService creates a new runner.
func NewGroupMonitorRunnerService(svc *GroupMonitorService) *GroupMonitorRunnerService {
	return &GroupMonitorRunnerService{svc: svc, instanceID: uuid.NewString()}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for each tick. When both are nil the job runs ungated.
func (s *GroupMonitorRunnerService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

// Start begins the cron ticker (every minute).
func (s *GroupMonitorRunnerService) Start() {
	if s == nil || s.svc == nil {
		return
	}
	s.startOnce.Do(func() {
		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(time.Local))
		_, err := c.AddFunc("* * * * *", func() { s.runScheduled() })
		if err != nil {
			logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] not started (invalid schedule): %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *GroupMonitorRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] cron stop timed out")
			}
		}
	})
}

func (s *GroupMonitorRunnerService) runScheduled() {
	if s.svc == nil {
		return
	}
	// 与 ScheduledTestRunner 一致，延迟 10s 落地在每分钟 ~:10。
	time.Sleep(10 * time.Second)

	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(
		lockCtx,
		s.lockCache,
		s.db,
		groupMonitorRunnerLeaderLockKey,
		s.instanceID,
		groupMonitorRunnerLeaderLockTTL,
	)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	s.pruneHistoryIfNeeded(ctx)

	now := time.Now()
	monitors, err := s.svc.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] ListDue error: %v", err)
		return
	}
	if len(monitors) == 0 {
		return
	}

	logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] found %d due monitors", len(monitors))

	sem := make(chan struct{}, groupMonitorDefaultMaxWorkers)
	var wg sync.WaitGroup
	for _, m := range monitors {
		sem <- struct{}{}
		wg.Add(1)
		go func(mon *GroupMonitor) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.svc.RunOneMonitor(ctx, mon); err != nil {
				logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] monitor=%d group=%d error: %v", mon.ID, mon.GroupID, err)
			}
		}(m)
	}
	wg.Wait()
}

// pruneHistoryIfNeeded 按间隔清理超过保留期的历史记录（仅 leader 实例执行）。
func (s *GroupMonitorRunnerService) pruneHistoryIfNeeded(ctx context.Context) {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()
	if time.Since(s.lastPrune) < groupMonitorPruneInterval {
		return
	}
	s.lastPrune = time.Now()

	n, err := s.svc.PruneHistory(ctx, groupMonitorHistoryRetention)
	if err != nil {
		logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] prune history error: %v", err)
		return
	}
	if n > 0 {
		logger.LegacyPrintf("service.group_monitor_runner", "[GroupMonitorRunner] pruned %d history rows (retention=%s)", n, groupMonitorHistoryRetention)
	}
}
