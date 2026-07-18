import { createContext, useContext } from 'react'

export type ToastVariant = 'success' | 'error' | 'info'

export interface ToastContextValue {
  toast: (message: string, variant?: ToastVariant) => void
}

export const ToastContext = createContext<ToastContextValue>({ toast: () => {} })

export function useToast() {
  return useContext(ToastContext)
}
