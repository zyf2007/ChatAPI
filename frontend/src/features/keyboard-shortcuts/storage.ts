import {
  KEYBOARD_SHORTCUT_ACTIONS,
  cloneKeyboardShortcutConfig,
  createDefaultKeyboardShortcutConfig,
  getKeyboardShortcutValidationIssues,
  isKeyboardShortcutBindingSafe,
  type KeyboardShortcutAction,
  type KeyboardShortcutBinding,
  type KeyboardShortcutConfig,
} from './shortcuts.ts'

const STORAGE_PREFIX = 'chatapi.keyboard-shortcuts.v1'
export const KEYBOARD_SHORTCUTS_CHANGED_EVENT = 'chatapi:keyboard-shortcuts-changed'

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseStoredBinding(value: unknown): KeyboardShortcutBinding | null | undefined {
  if (value === null) return null
  if (!isObject(value)) return undefined
  if (
    typeof value.key !== 'string'
    || typeof value.mod !== 'boolean'
    || typeof value.alt !== 'boolean'
    || typeof value.shift !== 'boolean'
  ) {
    return undefined
  }
  const binding = {
    key: value.key,
    mod: value.mod,
    alt: value.alt,
    shift: value.shift,
  }
  return isKeyboardShortcutBindingSafe(binding) ? binding : undefined
}

export function keyboardShortcutStorageKey(userID: string): string {
  const normalizedUserID = userID.trim()
  if (!normalizedUserID) throw new Error('缺少当前用户，无法读取快捷键配置')
  return `${STORAGE_PREFIX}.${normalizedUserID}`
}

export function sanitizeKeyboardShortcutConfig(value: unknown): KeyboardShortcutConfig {
  const fallback = createDefaultKeyboardShortcutConfig()
  if (!isObject(value) || value.version !== 1 || !isObject(value.bindings)) return fallback

  const bindings = { ...fallback.bindings }
  for (const action of KEYBOARD_SHORTCUT_ACTIONS) {
    if (!(action in value.bindings)) continue
    const binding = parseStoredBinding(value.bindings[action])
    if (binding === undefined) return fallback
    bindings[action] = binding
  }

  const config: KeyboardShortcutConfig = { version: 1, bindings }
  return getKeyboardShortcutValidationIssues(config).length
    ? fallback
    : cloneKeyboardShortcutConfig(config)
}

export function loadKeyboardShortcutConfig(userID: string): KeyboardShortcutConfig {
  if (typeof window === 'undefined' || !userID.trim()) return createDefaultKeyboardShortcutConfig()
  try {
    const stored = window.localStorage.getItem(keyboardShortcutStorageKey(userID))
    return stored ? sanitizeKeyboardShortcutConfig(JSON.parse(stored) as unknown) : createDefaultKeyboardShortcutConfig()
  } catch {
    return createDefaultKeyboardShortcutConfig()
  }
}

export function saveKeyboardShortcutConfig(userID: string, config: KeyboardShortcutConfig): void {
  const issues = getKeyboardShortcutValidationIssues(config)
  if (issues.length) throw new Error('快捷键配置包含无效或冲突的键位')
  const saved = cloneKeyboardShortcutConfig(config)
  try {
    window.localStorage.setItem(keyboardShortcutStorageKey(userID), JSON.stringify(saved))
  } catch {
    throw new Error('无法写入浏览器存储，请检查隐私设置或存储权限。')
  }
  window.dispatchEvent(new CustomEvent(KEYBOARD_SHORTCUTS_CHANGED_EVENT, {
    detail: { userID: userID.trim() },
  }))
}

export function setKeyboardShortcutBinding(
  config: KeyboardShortcutConfig,
  action: KeyboardShortcutAction,
  binding: KeyboardShortcutBinding | null,
): KeyboardShortcutConfig {
  return {
    version: 1,
    bindings: {
      ...config.bindings,
      [action]: binding ? { ...binding } : null,
    },
  }
}
