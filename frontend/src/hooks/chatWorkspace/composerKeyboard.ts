import {
  createDefaultKeyboardShortcutConfig,
  findKeyboardShortcutAction,
  type KeyboardShortcutBindings,
  type KeyboardShortcutEventLike,
} from '../../features/keyboard-shortcuts/shortcuts.ts'
import type { ComposerMode } from '../../types/chat'

export type ComposerKeyboardEventLike = KeyboardShortcutEventLike & {
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
  isEditorTarget?: boolean
  isComposerSurfaceTarget?: boolean
  hasDraftBuffer: boolean
  hasComposerText: boolean
}

export type ComposerKeyboardAction =
  | { type: 'ignore' }
  | { type: 'none' }
  | { type: 'newline' }
  | { type: 'restore_draft' }
  | { type: 'complete' }
  | { type: 'stream' }
  | { type: 'cycle_mode' }

export type ComposerEnterAction = ComposerKeyboardAction

const CYCLABLE_COMPOSER_MODES: ComposerMode[] = ['assistant_message', 'thinking', 'tool_call']

export function shouldIgnoreComposerEnter(event: ComposerKeyboardEventLike): boolean {
  return Boolean(event.isComposing || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229)
}

export function getNextComposerMode(currentMode: ComposerMode): ComposerMode {
  const currentIndex = CYCLABLE_COMPOSER_MODES.indexOf(currentMode)
  return CYCLABLE_COMPOSER_MODES[(currentIndex + 1) % CYCLABLE_COMPOSER_MODES.length]
}

export function shouldUseNativeComposerNewline(event: ComposerKeyboardEventLike): boolean {
  return event.key === 'Enter'
}

export function decideComposerKeyboardAction(
  event: ComposerKeyboardEventLike,
  context: ComposerKeyboardContext,
  bindings: KeyboardShortcutBindings,
): ComposerKeyboardAction {
  if (shouldIgnoreComposerEnter(event)) return { type: 'ignore' }

  const action = findKeyboardShortcutAction(event, bindings)
  if (!action) return { type: 'none' }

  const isEditorTarget = context.isEditorTarget !== false
  switch (action) {
    case 'newline':
      return isEditorTarget && (context.isAnswerMode || context.isThinkingMode)
        ? { type: 'newline' }
        : { type: 'none' }
    case 'restore_draft':
      if (
        !isEditorTarget
        || context.sending
        || !context.isWaitingForUser
        || !context.isAnswerMode
        || !context.hasDraftBuffer
      ) {
        return { type: 'none' }
      }
      return { type: 'restore_draft' }
    case 'complete':
      if (!isEditorTarget || context.sending || !context.isWaitingForUser || !context.isAnswerMode) {
        return { type: 'none' }
      }
      return { type: 'complete' }
    case 'stream': {
      const canStreamChunk = isEditorTarget && (context.isAnswerMode || context.isThinkingMode)
      if (
        context.sending
        || !context.isWaitingForUser
        || !canStreamChunk
        || !context.hasComposerText
      ) {
        return { type: 'none' }
      }
      return { type: 'stream' }
    }
    case 'cycle_mode':
      return context.sending
        || !context.isWaitingForUser
        || (!isEditorTarget && !context.isComposerSurfaceTarget)
        ? { type: 'none' }
        : { type: 'cycle_mode' }
  }
}

// Backward-compatible default policy retained for callers that only need the
// historical Enter behavior.
export function decideComposerEnterAction(
  event: ComposerKeyboardEventLike,
  context: ComposerKeyboardContext,
): ComposerKeyboardAction {
  return decideComposerKeyboardAction(
    event,
    context,
    createDefaultKeyboardShortcutConfig().bindings,
  )
}
