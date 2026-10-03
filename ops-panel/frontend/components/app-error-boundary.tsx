import { Component, type ErrorInfo, type ReactNode } from "react"
import { AlertTriangle, RefreshCw } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"

interface AppErrorBoundaryProps {
  children: ReactNode
}

interface AppErrorBoundaryState {
  error: Error | null
}

export class AppErrorBoundary extends Component<AppErrorBoundaryProps, AppErrorBoundaryState> {
  state: AppErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): AppErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Operations page render failed", error, info.componentStack)
    try {
      void fetch("/api/client-error", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          message: error.message,
          stack: error.stack ?? "",
          componentStack: info.componentStack ?? "",
        }),
      }).catch(() => undefined)
    } catch {
      // Error reporting must never break the fallback UI further.
    }
  }

  private describeError() {
    const message = this.state.error?.message?.trim()
    if (!message) return "未知渲染错误"
    return message.length > 240 ? `${message.slice(0, 240)}…` : message
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <main className="mx-auto flex min-h-screen max-w-2xl items-center px-4 py-8">
        <Alert variant="destructive">
          <AlertTriangle className="size-4" />
          <AlertTitle>页面加载失败</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>数据刷新过程中出现异常，当前页面没有被清空。</p>
            <p className="break-words font-mono text-xs text-destructive/80">{this.describeError()}</p>
            <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
              <RefreshCw className="size-4" />
              重新加载
            </Button>
          </AlertDescription>
        </Alert>
      </main>
    )
  }
}
