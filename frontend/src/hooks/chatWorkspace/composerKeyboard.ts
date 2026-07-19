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
