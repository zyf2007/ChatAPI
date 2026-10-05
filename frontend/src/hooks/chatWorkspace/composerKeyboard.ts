export type ComposerKeyboardEventLike = {
  key?: string
  altKey?: boolean
  ctrlKey?: boolean
  metaKey?: boolean
  shiftKey?: boolean
  isComposing?: boolean
  nativeEvent: {
    isComposing?: boolean
    keyCode?: number
  }
}

export type ComposerKeyboardContext = {
  sending: boolean
  isWaitingForUser: boolean
  isAnswerMode: boolean
  isThinkingMode: boolean
  hasDraftBuffer: boolean
  hasComposerText: boolean
}

export type ComposerEnterAction =
  | { type: 'ignore' }
  | { type: 'none' }
  | { type: 'newline' }
  | { type: 'restore_draft' }
  | { type: 'complete' }
  | { type: 'stream' }

// The composer's two primary modes. Tab toggles between them while the
// textarea is focused (composerKeyboard.test.ts freezes this two-state intent).
export const COMPOSER_TAB_MODES = ['assistant_message', 'thinking'] as const

export type ComposerTabMode = (typeof COMPOSER_TAB_MODES)[number]

export type ComposerTabAction = { type: 'none' } | { type: 'toggle'; nextMode: ComposerTabMode }

// decideComposerTabAction toggles the composer mode on Tab.
// Shift+Tab and Ctrl/Cmd/Alt+Tab are left to the browser so Tab/Shift+Tab keep
// their normal accessibility focus-navigation behavior.
export function decideComposerTabAction(
  event: ComposerKeyboardEventLike,
  currentMode: string,
): ComposerTabAction {
  if ((event.key ?? '') !== 'Tab') {
    return { type: 'none' }
  }
  if (event.shiftKey || event.ctrlKey || event.metaKey || event.altKey) {
    return { type: 'none' }
  }
  if (currentMode !== COMPOSER_TAB_MODES[0] && currentMode !== COMPOSER_TAB_MODES[1]) {
    return { type: 'none' }
  }
  return {
    type: 'toggle',
    nextMode: currentMode === COMPOSER_TAB_MODES[0] ? COMPOSER_TAB_MODES[1] : COMPOSER_TAB_MODES[0],
  }
}

export function shouldIgnoreComposerEnter(event: ComposerKeyboardEventLike): boolean {
  return Boolean(event.isComposing || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229)
}

// decideComposerEnterAction is the pure keyboard policy used by the workspace
// composer. Tests assert the full decision, not only the IME ignore half.
export function decideComposerEnterAction(
  event: ComposerKeyboardEventLike,
  context: ComposerKeyboardContext,
): ComposerEnterAction {
  if ((event.key ?? 'Enter') !== 'Enter') {
    return { type: 'none' }
  }
  if (shouldIgnoreComposerEnter(event)) {
    return { type: 'ignore' }
  }
  if (event.altKey) {
    if (context.sending || !context.isWaitingForUser || !context.isAnswerMode || !context.hasDraftBuffer) {
      return { type: 'none' }
    }
    return { type: 'restore_draft' }
  }
  if (event.ctrlKey || event.metaKey) {
    if (context.sending || !context.isWaitingForUser || !context.isAnswerMode) {
      return { type: 'none' }
    }
    return { type: 'complete' }
  }
  if (event.shiftKey) {
    return { type: 'newline' }
  }
  const canStreamChunk = context.isAnswerMode || context.isThinkingMode
  if (context.sending || !context.isWaitingForUser || !canStreamChunk || !context.hasComposerText) {
    return { type: 'none' }
  }
  return { type: 'stream' }
}
