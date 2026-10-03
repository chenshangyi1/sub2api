import type { ReactNode } from "react"
import { Navigate, useSearchParams } from "react-router-dom"

const legacyViewDestinations: Readonly<Record<string, string>> = {
  overview: "/",
  usage: "/target-analytics",
  "bl-sync": "/target-accounts",
  groups: "/target-accounts",
  accounts: "/target-accounts",
  monitor: "/target-probes",
  logs: "/diagnostics",
  "service-status": "/diagnostics",
  announcements: "/settings?tab=target-announcements",
  settings: "/settings?tab=system",
}

export function LegacyHomeRoute({ children }: { children: ReactNode }) {
  const [searchParams] = useSearchParams()
  const legacyView = searchParams.get("view")

  if (!legacyView) return children
  return <Navigate to={legacyViewDestinations[legacyView] ?? "/"} replace />
}
