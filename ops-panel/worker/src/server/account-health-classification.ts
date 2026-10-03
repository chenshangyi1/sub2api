export type AccountHealthResult = {
  status?: string | null;
  message?: string | null;
  firstTokenMs?: number | null;
};

export type AccountHealthDisposition =
  | "success"
  | "balance_exhausted"
  | "rate_limited"
  | "auth_failure"
  | "gateway_error"
  | "probe_failure"
  | "upstream_unknown";

function statusCodeFromMessage(message: string) {
  const match = message.match(/\b([45]\d{2})\b/);
  return match ? Number(match[1]) : 0;
}

function normalizedMessage(result: AccountHealthResult) {
  return result.message?.toLowerCase().replace(/\s+/g, " ").trim() ?? "";
}

export function classifyAccountHealthResult(result: AccountHealthResult): AccountHealthDisposition {
  if (result.status === "success") return "success";

  const message = normalizedMessage(result);
  const statusCode = statusCodeFromMessage(message);
  if (
    statusCode === 402
    || /balance(?:\s|_|-)*(?:exhausted|depleted|insufficient|empty|zero|not enough)|credit(?:\s|_|-)*(?:exhausted|depleted|insufficient)|quota(?:\s|_|-)*(?:exhausted|depleted|insufficient|reached)|usage(?:\s|_|-)*limit|insufficient(?:\s|_|-)*(?:balance|credit|funds|quota)|resource(?:\s|_|-)*exhausted|余额(?:不足|耗尽|为零)|额度(?:不足|耗尽)|用量(?:耗尽|已达上限)/.test(message)
  ) {
    return "balance_exhausted";
  }

  if (
    statusCode === 429
    || /too many requests|rate[ _-]?limit|overloaded|限流/.test(message)
  ) {
    return "rate_limited";
  }

  if (
    statusCode === 401
    || statusCode === 403
    || /auth|credential|invalid[ _-]?(?:api[ _-]?)?key|unauthorized|forbidden|凭据|认证/.test(message)
  ) {
    return "auth_failure";
  }

  if (statusCode >= 500 || /\b(?:500|502|503|504)\b|bad gateway|service unavailable|gateway/.test(message)) {
    return "gateway_error";
  }

  if (/timeout|timed out|deadline|connection|connect|network|probe|探测|超时|连接/.test(message)) {
    return "probe_failure";
  }

  return "upstream_unknown";
}

export function isRateLimitedAccountResult(result: AccountHealthResult) {
  return classifyAccountHealthResult(result) === "rate_limited";
}

export function isBalanceExhaustedAccountResult(result: AccountHealthResult) {
  return classifyAccountHealthResult(result) === "balance_exhausted";
}

export function isBreakerFailureAccountResult(result: AccountHealthResult) {
  const disposition = classifyAccountHealthResult(result);
  return disposition !== "success" && disposition !== "rate_limited" && disposition !== "balance_exhausted";
}

export function scoreAccountHealthResult(result: AccountHealthResult, slowFirstTokenMs: number) {
  const disposition = classifyAccountHealthResult(result);
  if (disposition === "success") {
    return result.firstTokenMs !== null && result.firstTokenMs !== undefined && result.firstTokenMs > slowFirstTokenMs ? 65 : 100;
  }
  if (disposition === "balance_exhausted") return 25;
  if (disposition === "rate_limited") return 25;
  if (disposition === "auth_failure") return 0;
  if (disposition === "gateway_error") return 25;
  if (disposition === "probe_failure") return 10;
  return 40;
}
