/**
 * Utilities for file download and upload in the browser.
 */

/** Triggers a browser download of a text string as a file. */
export function downloadText(filename: string, content: string, mime = 'text/plain'): void {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

/** Reads a File object and resolves with its text content. */
export function readTextFile(file: File): Promise<string> {
  return file.text()
}
