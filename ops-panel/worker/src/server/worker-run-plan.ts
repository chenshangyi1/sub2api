export type WorkerRunPlan = {
  cleanupLogs: boolean;
  balanceAlerts: boolean;
  upstreamMonitors: boolean;
  ratePolicies: boolean;
  accountPoolPolicies: boolean;
  forceCheckMode?: "light" | "heavy";
};

export function planWorkerRun(requestMode?: string | null): WorkerRunPlan {
  const mode = requestMode?.trim().toLowerCase() ?? "";
  if (mode.startsWith("probe")) {
    return {
      cleanupLogs: false,
      balanceAlerts: false,
      upstreamMonitors: true,
      ratePolicies: false,
      accountPoolPolicies: false,
      forceCheckMode: mode === "probe:light"
        ? "light"
        : mode === "probe:heavy"
          ? "heavy"
          : undefined,
    };
  }
  if (mode === "automation") {
    return {
      cleanupLogs: false,
      balanceAlerts: false,
      upstreamMonitors: false,
      ratePolicies: false,
      accountPoolPolicies: true,
    };
  }
  if (mode === "rate-rules") {
    return {
      cleanupLogs: false,
      balanceAlerts: false,
      upstreamMonitors: false,
      ratePolicies: true,
      accountPoolPolicies: false,
    };
  }
  return {
    cleanupLogs: true,
    balanceAlerts: true,
    upstreamMonitors: true,
    ratePolicies: true,
    accountPoolPolicies: true,
  };
}
