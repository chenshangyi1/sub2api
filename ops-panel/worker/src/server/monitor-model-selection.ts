const nonTextMonitorModelPattern = /(?:^|[-_.])(embedding|image|dall|tts|audio|realtime|transcri|moderation|whisper)(?:$|[-_.])/i;

const lowCostMonitorModelPatterns = [
  /(?:^|[-_.])(nano|micro|tiny|lite|free)(?:$|[-_.])/i,
  /(?:^|[-_.])(mini|haiku)(?:$|[-_.])/i,
  /(?:^|[-_.])flash(?:$|[-_.])/i,
];

function monitorModelCostPriority(modelId: string) {
  const tier = lowCostMonitorModelPatterns.findIndex((pattern) => pattern.test(modelId));
  return tier === -1 ? lowCostMonitorModelPatterns.length : tier;
}

export function selectMonitorModelCandidates(models: Array<{ id?: unknown }>, limit = 3) {
  const uniqueIds = Array.from(new Set(models
    .map((item) => typeof item.id === "string" ? item.id.trim() : "")
    .filter(Boolean)))
    .filter((id) => !nonTextMonitorModelPattern.test(id));

  return uniqueIds
    .map((id, index) => ({ id, index, priority: monitorModelCostPriority(id) }))
    .sort((left, right) => left.priority - right.priority || left.index - right.index)
    .slice(0, Math.min(10, Math.max(1, Math.floor(limit))))
    .map((item) => item.id);
}
