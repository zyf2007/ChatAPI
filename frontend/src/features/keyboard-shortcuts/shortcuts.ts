export const KEYBOARD_SHORTCUT_ACTIONS = [
  'stream',
  'complete',
  'newline',
  'restore_draft',
  'cycle_mode',
] as const

export type KeyboardShortcutAction = (typeof KEYBOARD_SHORTCUT_ACTIONS)[number]

export type KeyboardShortcutBinding = {
  key: string
  mod: boolean
  alt: boolean
  shift: boolean
}

export type KeyboardShortcutBindings = Record<KeyboardShortcutAction, KeyboardShortcutBinding | null>

export type KeyboardShortcutConfig = {
  version: 1
  bindings: KeyboardShortcutBindings
}

export type KeyboardShortcutEventLike = {
  key?: string
  altKey?: boolean
  ctrlKey?: boolean
  metaKey?: boolean
  shiftKey?: boolean
  getModifierState?: (key: 'AltGraph') => boolean
}

export type KeyboardShortcutValidationIssue =
  | { type: 'unsafe'; actions: KeyboardShortcutAction[] }
  | { type: 'duplicate'; actions: KeyboardShortcutAction[] }

const MODIFIER_KEYS = new Set(['Alt', 'AltGraph', 'Control', 'Meta', 'Shift'])
const REJECTED_KEYS = new Set(['Dead', 'Process', 'Unidentified'])
const SAFE_UNMODIFIED_KEYS = new Set(['Enter', 'Tab'])

const DEFAULT_BINDINGS: KeyboardShortcutBindings = {
  stream: { key: 'Enter', mod: false, alt: false, shift: false },
  complete: { key: 'Enter', mod: true, alt: false, shift: false },
  newline: { key: 'Enter', mod: false, alt: false, shift: true },
  restore_draft: { key: 'Enter', mod: false, alt: true, shift: false },
  cycle_mode: null,
}

function cloneBinding(binding: KeyboardShortcutBinding | null): KeyboardShortcutBinding | null {
  return binding ? { ...binding } : null
}

export function createDefaultKeyboardShortcutConfig(): KeyboardShortcutConfig {
  return {
    version: 1,
    bindings: {
      stream: cloneBinding(DEFAULT_BINDINGS.stream),
      complete: cloneBinding(DEFAULT_BINDINGS.complete),
      newline: cloneBinding(DEFAULT_BINDINGS.newline),
      restore_draft: cloneBinding(DEFAULT_BINDINGS.restore_draft),
      cycle_mode: cloneBinding(DEFAULT_BINDINGS.cycle_mode),
    },
  }
}

export function cloneKeyboardShortcutConfig(config: KeyboardShortcutConfig): KeyboardShortcutConfig {
  return {
    version: 1,
    bindings: {
      stream: cloneBinding(config.bindings.stream),
      complete: cloneBinding(config.bindings.complete),
      newline: cloneBinding(config.bindings.newline),
      restore_draft: cloneBinding(config.bindings.restore_draft),
      cycle_mode: cloneBinding(config.bindings.cycle_mode),
    },
  }
}

function normalizeKey(key: string | undefined): string {
  if (!key) return ''
  if (key === ' ') return 'Space'
  if (key === 'Esc') return 'Escape'
  if (key.length === 1) return key.toUpperCase()
  return key
}

export function isAltGraphModifierActive(event: KeyboardShortcutEventLike): boolean {
  return Boolean(event.getModifierState?.('AltGraph'))
}

export function normalizeKeyboardShortcutEvent(
  event: KeyboardShortcutEventLike,
): KeyboardShortcutBinding | null {
  const key = normalizeKey(event.key)
  if (!key || MODIFIER_KEYS.has(key) || isAltGraphModifierActive(event)) return null
  return {
    key,
    mod: Boolean(event.ctrlKey || event.metaKey),
    alt: Boolean(event.altKey),
    shift: Boolean(event.shiftKey),
  }
}

export function isKeyboardShortcutBindingSafe(binding: KeyboardShortcutBinding): boolean {
  const key = normalizeKey(binding.key)
  if (!key || key.length > 32 || MODIFIER_KEYS.has(key) || REJECTED_KEYS.has(key)) return false
  // Ctrl+Alt is emitted by AltGraph on many international layouts. Restrict
  // modified bindings to Enter so editing, browser, and OS shortcuts such as
  // Mod+Z, Mod+W, Alt+Arrow, or Alt+F4 can never become send actions.
  if (binding.mod && binding.alt) return false
  if (binding.mod || binding.alt) return key === 'Enter'
  return SAFE_UNMODIFIED_KEYS.has(key)
}

export function keyboardShortcutBindingID(binding: KeyboardShortcutBinding): string {
  return `${binding.mod ? '1' : '0'}:${binding.alt ? '1' : '0'}:${binding.shift ? '1' : '0'}:${normalizeKey(binding.key)}`
}

export function keyboardShortcutBindingsEqual(
  left: KeyboardShortcutBinding | null,
  right: KeyboardShortcutBinding | null,
): boolean {
  if (!left || !right) return left === right
  return keyboardShortcutBindingID(left) === keyboardShortcutBindingID(right)
}

export function keyboardShortcutConfigsEqual(
  left: KeyboardShortcutConfig,
  right: KeyboardShortcutConfig,
): boolean {
  return KEYBOARD_SHORTCUT_ACTIONS.every((action) =>
    keyboardShortcutBindingsEqual(left.bindings[action], right.bindings[action]),
  )
}

export function getKeyboardShortcutValidationIssues(
  config: KeyboardShortcutConfig,
): KeyboardShortcutValidationIssue[] {
  const issues: KeyboardShortcutValidationIssue[] = []
  const actionsByBinding = new Map<string, KeyboardShortcutAction[]>()

  for (const action of KEYBOARD_SHORTCUT_ACTIONS) {
    const binding = config.bindings[action]
    if (!binding) continue
    if (!isKeyboardShortcutBindingSafe(binding)) {
      issues.push({ type: 'unsafe', actions: [action] })
      continue
    }
    const id = keyboardShortcutBindingID(binding)
    actionsByBinding.set(id, [...(actionsByBinding.get(id) ?? []), action])
  }

  for (const actions of actionsByBinding.values()) {
    if (actions.length > 1) issues.push({ type: 'duplicate', actions })
  }
  return issues
}

export function findKeyboardShortcutAction(
  event: KeyboardShortcutEventLike,
  bindings: KeyboardShortcutBindings,
): KeyboardShortcutAction | null {
  const pressed = normalizeKeyboardShortcutEvent(event)
  if (!pressed) return null
  return KEYBOARD_SHORTCUT_ACTIONS.find((action) =>
    keyboardShortcutBindingsEqual(bindings[action], pressed),
  ) ?? null
}

export function formatKeyboardShortcutBinding(binding: KeyboardShortcutBinding | null): string {
  if (!binding) return '未设置'
  return [
    binding.mod ? 'Ctrl/⌘' : '',
    binding.alt ? 'Alt' : '',
    binding.shift ? 'Shift' : '',
    normalizeKey(binding.key),
  ].filter(Boolean).join('+')
}
