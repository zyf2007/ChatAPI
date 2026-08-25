import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  decideComposerEnterAction,
  decideComposerKeyboardAction,
  getNextComposerMode,
  shouldIgnoreComposerEnter,
  shouldUseNativeComposerNewline,
  type ComposerKeyboardContext,
  type ComposerKeyboardEventLike,
} from './composerKeyboard.ts'
import {
  createDefaultKeyboardShortcutConfig,
  getKeyboardShortcutValidationIssues,
  normalizeKeyboardShortcutEvent,
} from '../../features/keyboard-shortcuts/shortcuts.ts'
import { sanitizeKeyboardShortcutConfig } from '../../features/keyboard-shortcuts/storage.ts'

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

  it('keeps Shift+Enter as a native textarea newline', () => {
    const shiftEnter = event({ shiftKey: true })
    assert.deepEqual(decideComposerEnterAction(shiftEnter, baseContext), { type: 'newline' })
    assert.equal(shouldUseNativeComposerNewline(shiftEnter), true)
    assert.equal(shouldUseNativeComposerNewline(event({ key: 'Tab', shiftKey: true })), false)
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

describe('custom composer shortcuts', () => {
  it('supports Shift+Enter complete and Shift+Tab mode cycling when configured', () => {
    const config = createDefaultKeyboardShortcutConfig()
    config.bindings.complete = { key: 'Enter', mod: false, alt: false, shift: true }
    config.bindings.newline = { key: 'Enter', mod: true, alt: false, shift: true }
    config.bindings.cycle_mode = { key: 'Tab', mod: false, alt: false, shift: true }

    assert.deepEqual(
      decideComposerKeyboardAction(event({ shiftKey: true }), baseContext, config.bindings),
      { type: 'complete' },
    )
    assert.deepEqual(
      decideComposerKeyboardAction(event({ key: 'Tab', shiftKey: true }), {
        ...baseContext,
        isAnswerMode: false,
        isEditorTarget: false,
        isComposerSurfaceTarget: true,
      }, config.bindings),
      { type: 'cycle_mode' },
    )
  })

  it('preserves native Shift+Tab behavior until the user binds it', () => {
    const config = createDefaultKeyboardShortcutConfig()
    assert.deepEqual(
      decideComposerKeyboardAction(event({ key: 'Tab', shiftKey: true }), baseContext, config.bindings),
      { type: 'none' },
    )
  })

  it('does not run editor or cycle actions from nested tool controls', () => {
    const config = createDefaultKeyboardShortcutConfig()
    config.bindings.cycle_mode = { key: 'Tab', mod: false, alt: false, shift: true }
    assert.deepEqual(
      decideComposerKeyboardAction(event({}), {
        ...baseContext,
        isEditorTarget: false,
        isComposerSurfaceTarget: false,
      }, config.bindings),
      { type: 'none' },
    )
    assert.deepEqual(
      decideComposerKeyboardAction(event({ key: 'Tab', shiftKey: true }), {
        ...baseContext,
        isEditorTarget: false,
        isComposerSurfaceTarget: false,
      }, config.bindings),
      { type: 'none' },
    )
  })

  it('cycles only through assistant, thinking, and tool-call modes', () => {
    assert.equal(getNextComposerMode('assistant_message'), 'thinking')
    assert.equal(getNextComposerMode('thinking'), 'tool_call')
    assert.equal(getNextComposerMode('tool_call'), 'assistant_message')
    assert.equal(getNextComposerMode('builtin_tool'), 'assistant_message')
  })
})

describe('shortcut configuration validation', () => {
  it('normalizes Ctrl and Command to the portable Mod modifier', () => {
    assert.deepEqual(normalizeKeyboardShortcutEvent(event({ ctrlKey: true })), {
      key: 'Enter',
      mod: true,
      alt: false,
      shift: false,
    })
    assert.deepEqual(normalizeKeyboardShortcutEvent(event({ metaKey: true })), {
      key: 'Enter',
      mod: true,
      alt: false,
      shift: false,
    })
  })

  it('rejects printable, editing, browser, and AltGraph-like bindings', () => {
    const unsafeBindings = [
      { key: 'K', mod: false, alt: false, shift: false },
      { key: 'Z', mod: true, alt: false, shift: false },
      { key: 'W', mod: true, alt: false, shift: false },
      { key: 'K', mod: true, alt: true, shift: false },
      { key: 'ArrowLeft', mod: false, alt: true, shift: false },
      { key: 'F5', mod: false, alt: false, shift: false },
    ]
    for (const binding of unsafeBindings) {
      const config = createDefaultKeyboardShortcutConfig()
      config.bindings.cycle_mode = binding
      assert.equal(getKeyboardShortcutValidationIssues(config)[0]?.type, 'unsafe')
    }
    assert.equal(normalizeKeyboardShortcutEvent(event({
      key: '@',
      ctrlKey: true,
      altKey: true,
      getModifierState: (key) => key === 'AltGraph',
    })), null)
  })

  it('rejects duplicate bindings', () => {
    const config = createDefaultKeyboardShortcutConfig()
    config.bindings.cycle_mode = { ...config.bindings.stream! }
    assert.equal(getKeyboardShortcutValidationIssues(config)[0]?.type, 'duplicate')
  })

  it('falls back to defaults for malformed persisted settings', () => {
    const fallback = sanitizeKeyboardShortcutConfig({
      version: 1,
      bindings: {
        stream: { key: 'K', mod: false, alt: false, shift: false },
      },
    })
    assert.deepEqual(fallback, createDefaultKeyboardShortcutConfig())
  })

  it('keeps explicit unbound actions when persisted settings are valid', () => {
    const stored = createDefaultKeyboardShortcutConfig()
    stored.bindings.stream = null
    const parsed = sanitizeKeyboardShortcutConfig(stored)
    assert.equal(parsed.bindings.stream, null)
    assert.deepEqual(parsed.bindings.complete, createDefaultKeyboardShortcutConfig().bindings.complete)
  })
})
