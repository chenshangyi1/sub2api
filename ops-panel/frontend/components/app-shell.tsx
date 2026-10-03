"use client"

import { Outlet } from "react-router-dom"
import { MonitorHeader } from "@/components/monitor/monitor-header"

/**
 * AppShell 是所有路由共享的外壳：顶部 header + 中间 Outlet。
 */
export function AppShell() {
  return (
    <div className="min-h-screen bg-background">
      <MonitorHeader />
      <main className="mx-auto max-w-[120rem] space-y-4 px-3 py-3 sm:space-y-5 sm:px-6 sm:py-5 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}
