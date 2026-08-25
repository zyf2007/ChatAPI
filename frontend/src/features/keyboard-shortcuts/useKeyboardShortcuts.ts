import { useCallback, useMemo, useSyncExternalStore } from 'react'

import { createDefaultKeyboardShortcutConfig, type KeyboardShortcutConfig } from './shortcuts'
import {
  KEYBOARD_SHORTCUTS_CHANGED_EVENT,
  keyboardShortcutStorageKey,
  sanitizeKeyboardShortcutConfig,
} from './storage'

export function useKeyboardShortcuts(userID: string): KeyboardShortcutConfig {
  const normalizedUserID = userID.trim()

  const subscribe = useCallback((onStoreChange: () => void) => {
    if (typeof window === 'undefined' || !normalizedUserID) return () => undefined
    const expectedStorageKey = keyboardShortcutStorageKey(normalizedUserID)

    function handleLocalChange(event: Event) {
      const detail = (event as CustomEvent<{ userID?: string }>).detail
      if (detail?.userID === normalizedUserID) onStoreChange()
    }

    function handleStorage(event: StorageEvent) {
      if (event.key === null || event.key === expectedStorageKey) onStoreChange()
    }

    window.addEventListener(KEYBOARD_SHORTCUTS_CHANGED_EVENT, handleLocalChange)
    window.addEventListener('storage', handleStorage)
    return () => {
      window.removeEventListener(KEYBOARD_SHORTCUTS_CHANGED_EVENT, handleLocalChange)
      window.removeEventListener('storage', handleStorage)
    }
  }, [normalizedUserID])

  const getSnapshot = useCallback(() => {
    if (typeof window === 'undefined' || !normalizedUserID) return ''
    try {
      return window.localStorage.getItem(keyboardShortcutStorageKey(normalizedUserID)) ?? ''
    } catch {
      return ''
    }
  }, [normalizedUserID])

  const snapshot = useSyncExternalStore(subscribe, getSnapshot, () => '')
  return useMemo(() => {
    if (!snapshot) return createDefaultKeyboardShortcutConfig()
    try {
      return sanitizeKeyboardShortcutConfig(JSON.parse(snapshot) as unknown)
    } catch {
      return createDefaultKeyboardShortcutConfig()
    }
  }, [snapshot])
}
