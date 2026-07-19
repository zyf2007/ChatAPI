import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  decideComposerEnterAction,
  shouldIgnoreComposerEnter,
  type ComposerKeyboardContext,
  type ComposerKeyboardEventLike,
} from './composerKeyboard.ts'

const baseContext: ComposerKeyboardContext = {
  sending: false,
  isWaitingForUser: true,
  isAnswerMode: true,
  isThinkingMode: false,
  hasDraftBuffer: true,
  hasComposerText: true,
}

function event(partial: Partial<ComposerKeyboardEventLike> & { nativeEvent?: ComposerKeyboardEventLike['nativeEvent'] }): ComposerKeyboardEventLike {
  return {
    key: 'Enter',
    altKey: false,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    isComposing: false,
    ...partial,
    nativeEvent: {
      isComposing: false,
      keyCode: 13,
      ...(partial.nativeEvent ?? {}),
    },
  }
}

describe('shouldIgnoreComposerEnter', () => {
  it('suppresses React isComposing', () => {
    assert.equal(shouldIgnoreComposerEnter(event({ isComposing: true })), true)
  })

  it('suppresses native isComposing', () => {
    assert.equal(shouldIgnoreComposerEnter(event({ nativeEvent: { isComposing: true } })), true)
  })

  it('suppresses keyCode 229 IME marker', () => {
    assert.equal(shouldIgnoreComposerEnter(event({ nativeEvent: { keyCode: 229 } })), true)
  })

  it('allows ordinary Enter', () => {
    assert.equal(shouldIgnoreComposerEnter(event({})), false)
  })
})

describe('decideComposerEnterAction', () => {
  it('ignores IME composition even when other modifiers are present', () => {
    assert.deepEqual(
      decideComposerEnterAction(event({ isComposing: true, ctrlKey: true }), baseContext),
      { type: 'ignore' },
    )
  })

  it('streams ordinary Enter in answer mode', () => {
    assert.deepEqual(decideComposerEnterAction(event({}), baseContext), { type: 'stream' })
  })

  it('streams ordinary Enter in thinking mode', () => {
    assert.deepEqual(
      decideComposerEnterAction(event({}), {
        ...baseContext,
        isAnswerMode: false,
        isThinkingMode: true,
      }),
      { type: 'stream' },
    )
  })

  it('keeps Shift+Enter as newline', () => {
    assert.deepEqual(decideComposerEnterAction(event({ shiftKey: true }), baseContext), { type: 'newline' })
  })

  it('completes on Ctrl/Cmd+Enter only in answer mode', () => {
    assert.deepEqual(decideComposerEnterAction(event({ ctrlKey: true }), baseContext), { type: 'complete' })
    assert.deepEqual(decideComposerEnterAction(event({ metaKey: true }), baseContext), { type: 'complete' })
    assert.deepEqual(
      decideComposerEnterAction(event({ ctrlKey: true }), {
        ...baseContext,
        isAnswerMode: false,
        isThinkingMode: true,
      }),
      { type: 'none' },
    )
  })

  it('restores draft only with Alt+Enter in answer mode when draft exists', () => {
    assert.deepEqual(decideComposerEnterAction(event({ altKey: true }), baseContext), { type: 'restore_draft' })
    assert.deepEqual(
      decideComposerEnterAction(event({ altKey: true }), { ...baseContext, hasDraftBuffer: false }),
      { type: 'none' },
    )
  })

  it('does nothing while sending or when the turn is closed', () => {
    assert.deepEqual(
      decideComposerEnterAction(event({}), { ...baseContext, sending: true }),
      { type: 'none' },
    )
    assert.deepEqual(
      decideComposerEnterAction(event({}), { ...baseContext, isWaitingForUser: false }),
      { type: 'none' },
    )
  })
})
