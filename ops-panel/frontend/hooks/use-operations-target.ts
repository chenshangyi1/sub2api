import { useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import type { UpstreamSyncTarget } from "@/lib/api-types"
import { listOperationsTargets } from "@/lib/operations-api"

export function useOperationsTarget() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [targets, setTargets] = useState<UpstreamSyncTarget[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const requestedTargetID = Number(searchParams.get("target"))

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    listOperationsTargets()
      .then((items) => {
        if (!cancelled) setTargets(Array.isArray(items) ? items : [])
      })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setError(reason instanceof Error ? reason.message : "目标站点加载失败")
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload])

  const selectedTargetID = targets.some((target) => target.id === requestedTargetID)
    ? requestedTargetID
    : (targets.find((target) => target.enabled)?.id ?? targets[0]?.id ?? null)

  useEffect(() => {
    if (selectedTargetID == null || requestedTargetID === selectedTargetID) return
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.set("target", String(selectedTargetID))
        return next
      },
      { replace: true },
    )
  }, [requestedTargetID, selectedTargetID, setSearchParams])

  function selectTarget(targetID: number) {
    const hasRequestedTarget = targets.some((target) => target.id === requestedTargetID)
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.set("target", String(targetID))
        return next
      },
      { replace: !hasRequestedTarget },
    )
  }

  return {
    targets,
    selectedTargetID,
    selectedTarget: targets.find((target) => target.id === selectedTargetID) ?? null,
    loading,
    error,
    selectTarget,
    refetch: () => setReload((value) => value + 1),
  }
}
