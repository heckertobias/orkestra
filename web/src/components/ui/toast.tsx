import { useState, useCallback, useEffect } from 'react'
import { X, CheckCircle, AlertCircle, Info } from 'lucide-react'
import { ToastContext, type ToastVariant } from './toast-context'

interface Toast {
  id: string
  message: string
  variant: ToastVariant
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const toast = useCallback((message: string, variant: ToastVariant = 'info') => {
    const id = Math.random().toString(36).slice(2)
    setToasts(prev => [...prev, { id, message, variant }])
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
    }, 4000)
  }, [])

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-4 right-4 flex flex-col gap-2 z-50 max-w-sm">
        {toasts.map(t => (
          <ToastItem key={t.id} toast={t} onDismiss={() => setToasts(prev => prev.filter(x => x.id !== t.id))} />
        ))}
      </div>
    </ToastContext.Provider>
  )
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  const icons = {
    success: <CheckCircle size={16} style={{ color: 'var(--online)' }} />,
    error:   <AlertCircle size={16} style={{ color: 'var(--error)' }} />,
    info:    <Info size={16} style={{ color: 'var(--accent)' }} />,
  }

  useEffect(() => {
    // Fade in
  }, [])

  return (
    <div
      className="flex items-start gap-3 px-4 py-3 rounded-lg border text-sm shadow-lg"
      style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)', color: 'var(--text)' }}
    >
      {icons[toast.variant]}
      <span className="flex-1">{toast.message}</span>
      <button onClick={onDismiss} className="shrink-0 opacity-60 hover:opacity-100" style={{ color: 'var(--text-muted)' }}>
        <X size={14} />
      </button>
    </div>
  )
}
