/**
 * 复制文本到剪贴板。
 * 优先 Clipboard API；在 HTTP / 非安全上下文 / 权限受限时回退到 textarea + execCommand。
 */
export async function copyText(text: string): Promise<void> {
  const value = text ?? ""
  const writeClipboard = navigator.clipboard?.writeText?.bind(navigator.clipboard)
  if (writeClipboard) {
    try {
      await writeClipboard(value)
      return
    } catch {
      // fall through to legacy path
    }
  }

  if (typeof document === "undefined") {
    throw new Error("clipboard unavailable")
  }

  const textarea = document.createElement("textarea")
  textarea.value = value
  textarea.setAttribute("readonly", "")
  textarea.style.position = "fixed"
  textarea.style.left = "-9999px"
  textarea.style.top = "0"
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  textarea.setSelectionRange(0, value.length)
  let ok = false
  try {
    ok = document.execCommand("copy")
  } finally {
    document.body.removeChild(textarea)
  }
  if (!ok) {
    throw new Error("复制失败")
  }
}
