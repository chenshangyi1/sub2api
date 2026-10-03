package service

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 分组监控业务错误（统一声明，避免散落）。
var (
	ErrGroupMonitorNotFound = infraerrors.NotFound(
		"GROUP_MONITOR_NOT_FOUND", "group monitor not found",
	)
	ErrGroupMonitorInvalidGroup = infraerrors.BadRequest(
		"GROUP_MONITOR_INVALID_GROUP", "group_id is required",
	)
	ErrGroupMonitorInvalidInterval = infraerrors.BadRequest(
		"GROUP_MONITOR_INVALID_INTERVAL", "interval_minutes must be between 5 and 1440",
	)
	ErrGroupMonitorInvalidOutputTokens = infraerrors.BadRequest(
		"GROUP_MONITOR_INVALID_OUTPUT_TOKENS", "max_output_tokens must be between 1 and 256",
	)
	ErrGroupMonitorDuplicateGroup = infraerrors.Conflict(
		"GROUP_MONITOR_DUPLICATE_GROUP", "a monitor already exists for this group",
	)
	ErrGroupMonitorNoProbe = infraerrors.InternalServer(
		"GROUP_MONITOR_NO_PROBE", "account test service is not available",
	)
)
