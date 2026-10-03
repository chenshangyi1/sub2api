/** Read localStorage without throwing in Safari private mode / blocked storage. */
export function readLocalStorage(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

/** Write localStorage without throwing in Safari private mode / blocked storage. */
export function writeLocalStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Ignore quota / privacy / disabled-storage failures.
  }
}
