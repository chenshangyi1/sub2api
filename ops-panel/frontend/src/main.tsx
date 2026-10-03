import { lazy, StrictMode, Suspense, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import '@fontsource-variable/geist'
import '@fontsource-variable/geist-mono'
import { ThemeProvider } from '@/components/theme-provider'
import { AuthProvider } from '@/lib/auth-context'
import { RefreshProvider } from '@/lib/refresh-context'
import { AddChannelProvider } from '@/lib/add-channel-context'
import { AuthGate } from '@/components/auth/auth-gate'
import { AppShell } from '@/components/app-shell'
import { AppErrorBoundary } from '@/components/app-error-boundary'
import { LegacyHomeRoute } from '@/components/legacy-route-redirect'
import { Skeleton } from '@/components/ui/skeleton'
import { Toaster } from '@/components/ui/sonner'
import '@/app/globals.css'

// Browser page translation and DOM-mutating extensions rewrite text nodes out
// from under React's reconciler. React then throws NotFoundError on
// removeChild/insertBefore during the next refresh re-render, which trips the
// global error boundary ("页面加载失败"). Swallow only that specific
// DOMException so a refresh never nukes the whole panel.
function patchDomReconciliation() {
  const originalRemoveChild = Node.prototype.removeChild
  const originalInsertBefore = Node.prototype.insertBefore
  Node.prototype.removeChild = function <T extends Node>(this: Node, child: T): T {
    try {
      return originalRemoveChild.call(this, child) as T
    } catch (error) {
      if (error instanceof DOMException && error.name === 'NotFoundError') {
        return child
      }
      throw error
    }
  }
  Node.prototype.insertBefore = function <T extends Node>(
    this: Node,
    node: T,
    child: Node | null,
  ): T {
    try {
      return originalInsertBefore.call(this, node, child) as T
    } catch (error) {
      if (error instanceof DOMException && error.name === 'NotFoundError') {
        return node
      }
      throw error
    }
  }
}
patchDomReconciliation()

const DashboardPage = lazy(() => import('@/app/page'))
const GatewayPage = lazy(() => import('@/app/gateway-page'))
const SettingsPage = lazy(() => import('@/app/settings-page'))
const TargetAccountsPage = lazy(() => import('@/app/target-accounts-page'))
const TargetProbesPage = lazy(() => import('@/app/target-probes-page'))
const TargetAnalyticsPage = lazy(() => import('@/app/target-analytics-page'))
const DiagnosticsPage = lazy(() => import('@/app/diagnostics-page'))
const MediaPage = lazy(() => import('@/app/media-page'))
const GroupMonitorPage = lazy(() => import('@/app/group-monitor-page'))

function RouteFallback() {
  return (
    <div className="space-y-3" aria-busy="true">
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-48 w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  )
}

function LazyRoute({ children }: { children: ReactNode }) {
  return <Suspense fallback={<RouteFallback />}>{children}</Suspense>
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider attribute="class" defaultTheme="light" enableSystem disableTransitionOnChange>
      <AuthProvider>
        <AuthGate>
          <RefreshProvider>
            <BrowserRouter>
              <AddChannelProvider>
                <AppErrorBoundary>
                  <Routes>
                    <Route element={<AppShell />}>
                      <Route
                        index
                        element={
                          <LazyRoute>
                            <LegacyHomeRoute>
                              <DashboardPage />
                            </LegacyHomeRoute>
                          </LazyRoute>
                        }
                      />
<Route path="gateway" element={<LazyRoute><GatewayPage /></LazyRoute>} />
                      <Route path="settings" element={<LazyRoute><SettingsPage /></LazyRoute>} />
                      <Route path="target-accounts" element={<LazyRoute><TargetAccountsPage /></LazyRoute>} />
                      <Route path="target-probes" element={<LazyRoute><TargetProbesPage /></LazyRoute>} />
                      <Route path="target-analytics" element={<LazyRoute><TargetAnalyticsPage /></LazyRoute>} />
                      <Route path="group-monitor" element={<LazyRoute><GroupMonitorPage /></LazyRoute>} />
<Route path="diagnostics" element={<LazyRoute><DiagnosticsPage /></LazyRoute>} />
                      <Route path="media" element={<LazyRoute><MediaPage /></LazyRoute>} />
                      <Route path="login" element={<Navigate to="/" replace />} />
                      <Route path="setup" element={<Navigate to="/" replace />} />
                      <Route path="*" element={<Navigate to="/" replace />} />
                    </Route>
                  </Routes>
                </AppErrorBoundary>
              </AddChannelProvider>
            </BrowserRouter>
          </RefreshProvider>
          <Toaster richColors closeButton position="top-right" />
        </AuthGate>
      </AuthProvider>
    </ThemeProvider>
  </StrictMode>,
)
