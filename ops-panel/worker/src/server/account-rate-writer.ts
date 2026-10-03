import type { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { getAccountRate } from "@/server/account-utils";
import { normalizeRateMultiplier, ratesEqual } from "@/shared/rates";

export const MANUAL_ZERO_RATE_REASON = "account rate is manually locked at zero";

export function updateAccountRateMultiplierField(input: {
  client: Pick<Sub2ApiAdminClient, "updateAccountRateMultiplier">;
  accountId: number;
  rateMultiplier: number;
}) {
  return input.client.updateAccountRateMultiplier(input.accountId, input.rateMultiplier);
}

export async function writeAccountRateMultiplierPreservingManualZero(input: {
  client: Pick<Sub2ApiAdminClient, "getAccount" | "updateAccountRateMultiplier">;
  accountId: number;
  rateMultiplier: number;
  initialAccount?: unknown;
}) {
  const nextRate = normalizeRateMultiplier(input.rateMultiplier);
  const initialRate = input.initialAccount === undefined ? null : getAccountRate(input.initialAccount);
  if (initialRate !== null && ratesEqual(initialRate, 0)) {
    return { status: "skipped" as const, reason: MANUAL_ZERO_RATE_REASON, account: input.initialAccount, update: null };
  }

  const latestAccount = await input.client.getAccount(input.accountId);
  const latestRate = getAccountRate(latestAccount);
  if (latestRate === null) {
    throw new Error(`account ${input.accountId} detail response is missing rate_multiplier or contains an invalid value`);
  }
  if (ratesEqual(latestRate, 0)) {
    return { status: "skipped" as const, reason: MANUAL_ZERO_RATE_REASON, account: latestAccount, update: null };
  }
  if (ratesEqual(latestRate, nextRate)) {
    return { status: "unchanged" as const, reason: null, account: latestAccount, update: null };
  }

  const update = await updateAccountRateMultiplierField({
    client: input.client,
    accountId: input.accountId,
    rateMultiplier: nextRate,
  });
  const account = latestAccount && typeof latestAccount === "object"
    ? { ...(latestAccount as Record<string, unknown>), rate_multiplier: nextRate }
    : latestAccount;
  return { status: "updated" as const, reason: null, account, update };
}
