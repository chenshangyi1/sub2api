import { useEffect, useRef } from "react"
import { useSearchParams } from "react-router-dom"

export function useQueryTab<const TValues extends readonly string[]>(
  paramName: string,
  allowedValues: TValues,
  defaultValue: TValues[number],
): readonly [TValues[number], (value: TValues[number]) => void] {
  const [searchParams, setSearchParams] = useSearchParams()
  const pendingValueRef = useRef<TValues[number] | null>(null)
  const requestedValue = searchParams.get(paramName)
  const activeValue = allowedValues.some((value) => value === requestedValue)
    ? (requestedValue as TValues[number])
    : defaultValue

  useEffect(() => {
    if (requestedValue === pendingValueRef.current) pendingValueRef.current = null
  }, [requestedValue])

  useEffect(() => {
    if (requestedValue === activeValue) return
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.set(paramName, activeValue)
        return next
      },
      { replace: true },
    )
  }, [activeValue, paramName, requestedValue, setSearchParams])

  function setActiveValue(value: TValues[number]) {
    if (value === activeValue || value === pendingValueRef.current) return
    pendingValueRef.current = value
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      next.set(paramName, value)
      return next
    })
  }

  return [activeValue, setActiveValue] as const
}
