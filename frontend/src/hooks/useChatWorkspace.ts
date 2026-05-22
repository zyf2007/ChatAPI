import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'

import { requestJson } from '../lib/api'
import { appMessage } from '../lib/antdApp'
import {
  buildInitialToolFormValues,
  getLastToolSchemas,
} from '../lib/chat-format'
import { buildVisibleMessages } from '../lib/visibleMessages'
import { buildToolCallPayload } from './chatWorkspace/buildToolCallPayload'
import { DEFAULT_AUTH_SESSION } from './chatWorkspace/defaultAuthSession'
import { useConversationMessages } from './chatWorkspace/useConversationMessages'
import { useAutomationRules } from './useAutomationRules'
import { useKeyboardOffset } from './useKeyboardOffset'
import { useWorkspaceRealtime } from './useWorkspaceRealtime'
import type {
  AuthSession,
  AuthUser,
  ComposerMode,
  Conversation,
  ResponsesPayload,
  ToolFieldValue,
  MessageItem,
} from '../types/chat'

export function useChatWorkspace(isMobile: boolean) {
  const [booting, setBooting] = useState(true)
  const [auth, setAuth] = useState<AuthSession>(DEFAULT_AUTH_SESSION)
  const [loginLoading, setLoginLoading] = useState(false)
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedConversationId, setSelectedConversationId] = useState('')
  const [messagesByConversation, setMessagesByConversation] = useState<Record<string, MessageItem[]>>({})
  const [loadedConversationIds, setLoadedConversationIds] = useState<Set<string>>(() => new Set())
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [composer, setComposer] = useState('')
  const [thinkingText, setThinkingText] = useState('')
  const [composerMode, setComposerMode] = useState<ComposerMode>('assistant_message')
  const [toolName, setToolName] = useState('')
  const [toolCallId, setToolCallId] = useState('')
  const [toolFormValues, setToolFormValues] = useState<Record<string, ToolFieldValue>>({})
  const [draftBuffers, setDraftBuffers] = useState<Record<string, string>>({})
  const [sending, setSending] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [deletingConversationId, setDeletingConversationId] = useState('')
  const [pruneModalOpen, setPruneModalOpen] = useState(false)
  const [pruneKeepCount, setPruneKeepCount] = useState<number>(20)
  const [pruningConversations, setPruningConversations] = useState(false)
  const [abortingConversationId, setAbortingConversationId] = useState('')
  const [abortPopoverConversationId, setAbortPopoverConversationId] = useState('')
  const [abortReason, setAbortReason] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)
  const chatScrollRef = useRef<HTMLDivElement | null>(null)
  const keyboardOffset = useKeyboardOffset()
  const automation = useAutomationRules()

  const handleConnectionCountChange = useCallback((value: number) => {
    setAuth((current) =>
      current.current_connection_count === value
        ? current
        : {
            ...current,
            current_connection_count: value,
          },
    )
  }, [])

  const { applySelectedConversation } = useWorkspaceRealtime({
    authenticated: auth.authenticated,
    conversations,
    onConnectionCountChange: handleConnectionCountChange,
    selectedConversationId,
    setConversations,
    setDraftBuffers,
    setLoadedConversationIds,
    setMessagesByConversation,
    setMessagesLoading,
    setSelectedConversationId,
  })

  const selectedConversation = conversations.find(
    (item) => item.id === selectedConversationId,
  )
  const messages = messagesByConversation[selectedConversationId] ?? []
  const hasLocalDraftBuffer =
    !!selectedConversationId &&
    Object.prototype.hasOwnProperty.call(draftBuffers, selectedConversationId)
  const draftBuffer = hasLocalDraftBuffer
    ? draftBuffers[selectedConversationId] ?? ''
    : selectedConversation?.metadata?.realtime_draft_text ?? ''
  const isWaitingForUser = selectedConversation?.metadata?.realtime_status === 'waiting'
  const availableToolSchemas = getLastToolSchemas(messages)
  const selectedToolSchema =
    availableToolSchemas.find((item) => item.name === toolName) ?? null
  const visibleMessages = buildVisibleMessages(messages, draftBuffer)

  function setDraftBufferForConversation(conversationId: string, value: string) {
    if (!conversationId) return
    setDraftBuffers((prev) => ({
      ...prev,
      [conversationId]: value,
    }))
  }

  function buildAssistantTextWithThinking(answerText: string, reasoningText: string) {
    const normalizedAnswer = answerText.trim()
    const normalizedReasoning = reasoningText.trim()
    if (!normalizedReasoning) return normalizedAnswer
    const thinkingBlock = `<think>
${normalizedReasoning}
</think>`
    return normalizedAnswer ? `${thinkingBlock}

${normalizedAnswer}` : thinkingBlock
  }

  function clearThinkingInput() {
    setThinkingText('')
  }

  function withDraftSeparator(text: string) {
    if (!draftBuffer.trim()) return text
    return text.startsWith('\n') ? text : `\n\n${text}`
  }

  useEffect(() => {
    let active = true

    async function bootstrapPage() {
      setBooting(true)
      try {
        const session = await requestJson<AuthSession>('/api/auth/session')
        if (!active) return
        setAuth(session)
        setTotpEnabled(session.totp_enabled)
        if (session.authenticated) {
          await automation.loadAutomationRules()
        }
      } catch (error) {
        if (active) {
          appMessage.error(error instanceof Error ? error.message : '初始化失败')
        }
      } finally {
        if (active) {
          setBooting(false)
        }
      }
    }

    void bootstrapPage()

    return () => {
      active = false
    }
  }, [])

  useConversationMessages({
    authenticated: auth.authenticated,
    loadedConversationIds,
    selectedConversationId,
    setLoadedConversationIds,
    setMessagesByConversation,
    setMessagesLoading,
  })

  useEffect(() => {
    if (composerMode !== 'tool_call') return
    if (toolName && selectedToolSchema) return
    if (availableToolSchemas[0]?.name) {
      setToolName(availableToolSchemas[0].name)
    }
  }, [availableToolSchemas, composerMode, selectedToolSchema, toolName])

  useEffect(() => {
    setToolFormValues(buildInitialToolFormValues(selectedToolSchema?.parameters))
  }, [selectedToolSchema?.name])

  async function handleLogin(values: { username: string; password: string; totp?: string }) {
    setLoginLoading(true)
    try {
      await requestJson<{ ok: boolean; user: AuthUser }>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify(values),
      })
      const session = await requestJson<AuthSession>('/api/auth/session')
      setAuth(session)
      await automation.loadAutomationRules()
      appMessage.success('登录成功')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '登录失败')
    } finally {
      setLoginLoading(false)
    }
  }

  async function handleLogout() {
    try {
      await requestJson('/api/auth/logout', { method: 'POST' })
    } finally {
      setAuth(DEFAULT_AUTH_SESSION)
      setTotpEnabled(false)
      setConversations([])
      setSelectedConversationId('')
      setMessagesByConversation({})
      setLoadedConversationIds(new Set())
      setMessagesLoading(false)
      setComposer('')
      clearThinkingInput()
      setComposerMode('assistant_message')
      setToolName('')
      setToolCallId('')
      setToolFormValues({})
      setDraftBuffers({})
      automation.resetAutomationRuleUi()
      automation.setAutomationRules([])
      localStorage.removeItem('chatapi.conversationId')
      appMessage.info('已退出登录')
    }
  }

  async function handleTotpRefresh() {
    try {
      const session = await requestJson<AuthSession>('/api/auth/session')
      setTotpEnabled(session.totp_enabled)
    } catch {
      // ignore
    }
  }

  async function handleSelectConversation(conversationId: string) {
    if (conversationId === selectedConversationId) {
      if (isMobile) setDrawerOpen(false)
      return
    }
    applySelectedConversation(conversationId)
    if (isMobile) setDrawerOpen(false)
    setComposerMode('assistant_message')
    clearThinkingInput()
    setToolName('')
    setToolCallId('')
    setToolFormValues({})
  }

  async function handleDeleteConversation(conversationId: string) {
    const targetConversation = conversations.find((item) => item.id === conversationId)
    if (targetConversation?.metadata?.realtime_status === 'waiting') {
      appMessage.warning('等待中的会话不允许删除')
      return
    }

    setDeletingConversationId(conversationId)
    try {
      await requestJson(`/api/conversations/${conversationId}`, {
        method: 'DELETE',
      })
      appMessage.success('会话已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除会话失败')
    } finally {
      setDeletingConversationId('')
    }
  }

  async function handlePruneConversations() {
    if (!Number.isInteger(pruneKeepCount) || pruneKeepCount < 0) {
      appMessage.warning('请输入大于等于 0 的整数')
      return
    }

    setPruningConversations(true)
    try {
      const response = await requestJson<{
        deleted_count: number
        skipped_count: number
        keep_count: number
      }>('/api/conversations/prune', {
        method: 'POST',
        body: JSON.stringify({
          keep_count: pruneKeepCount,
        }),
      })

      setPruneModalOpen(false)

      if (response.skipped_count > 0) {
        appMessage.success(
          `已删除 ${response.deleted_count} 个会话，跳过 ${response.skipped_count} 个等待中的旧会话`,
        )
        return
      }
      appMessage.success(`已删除 ${response.deleted_count} 个会话`)
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '批量删除会话失败')
    } finally {
      setPruningConversations(false)
    }
  }

  async function handleAbortConversation(conversationId: string) {
    const reason = abortReason.trim()
    if (!reason) {
      appMessage.warning('请输入 abort 错误信息')
      return
    }

    setAbortingConversationId(conversationId)
    try {
      await requestJson(`/api/conversations/${conversationId}/abort`, {
        method: 'POST',
        body: JSON.stringify({ error: reason }),
      })
      setAbortPopoverConversationId('')
      setAbortReason('')
      appMessage.success('已 abort 该请求')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : 'Abort 失败')
    } finally {
      setAbortingConversationId('')
    }
  }

  async function handleDraft() {
    if (!isWaitingForUser) return
    if (composerMode === 'tool_call') {
      await handleSend({ resetMode: true, successMessage: '已输出 Tool Call' })
      return
    }
    const isThinkingMode = composerMode === 'thinking'
    const rawChunk = isThinkingMode
      ? buildAssistantTextWithThinking('', thinkingText.trim())
      : composer.trim()
    if (!rawChunk) return
    const chunk = withDraftSeparator(rawChunk)
    try {
      const response = await requestJson<{
        draft_text?: string
        draft_length: number
      }>('/api/chat/output/delta', {
        method: 'POST',
        body: JSON.stringify({
          text: chunk,
          conversation_id: selectedConversationId || undefined,
        }),
      })
      setDraftBufferForConversation(
        selectedConversationId,
        typeof response.draft_text === 'string' ? response.draft_text : `${draftBuffer}${chunk}`,
      )
      if (isThinkingMode) {
        clearThinkingInput()
      } else {
        setComposer('')
      }
      appMessage.success(isThinkingMode ? '已输出思考' : '已输出片段')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '输出片段失败')
    }
  }

  async function handleSend(options?: {
    resetMode?: boolean
    successMessage?: string
  }) {
    if (!isWaitingForUser) return
    const finalText =
      composerMode === 'assistant_message'
        ? ''
        : (() => {
            try {
              return buildToolCallPayload({
                selectedToolSchema,
                toolFormValues,
                toolName,
              })
            } catch (error) {
              appMessage.error(error instanceof Error ? error.message : '工具参数格式错误')
              return ''
            }
          })()
    const pendingChunk = composerMode === 'assistant_message' ? composer.trim() : ''

    if (composerMode === 'assistant_message' && !draftBuffer.trim() && !pendingChunk) {
      return
    }

    if (composerMode === 'tool_call' && !finalText) {
      return
    }

    setSending(true)
    try {
      if (composerMode === 'assistant_message' && pendingChunk) {
        const outputChunk = withDraftSeparator(pendingChunk)
        const draftResponse = await requestJson<{
          draft_text?: string
          draft_length: number
        }>('/api/chat/output/delta', {
          method: 'POST',
          body: JSON.stringify({
            text: outputChunk,
            conversation_id: selectedConversationId || undefined,
          }),
        })
        setDraftBufferForConversation(
          selectedConversationId,
          typeof draftResponse.draft_text === 'string'
            ? draftResponse.draft_text
            : `${draftBuffer}${outputChunk}`,
        )
      }

      setDraftBufferForConversation(selectedConversationId, '')
      const payload = {
        text: composerMode === 'tool_call' ? finalText : undefined,
        mode: composerMode,
        tool_name: composerMode === 'tool_call' ? toolName.trim() || undefined : undefined,
        tool_call_id:
          composerMode === 'tool_call' ? toolCallId.trim() || undefined : undefined,
        conversation_id: selectedConversationId || undefined,
      }
      const response = await requestJson<ResponsesPayload>('/api/chat/output/complete', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
      const nextConversationId = response.conversation?.id ?? selectedConversationId
      if (nextConversationId) {
        applySelectedConversation(nextConversationId)
      }
      setComposer('')
      if (options?.resetMode !== false) {
        setComposerMode('assistant_message')
      }
      setToolName('')
      setToolCallId('')
      setToolFormValues({})
      appMessage.success(options?.successMessage || '已结束输出')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '发送失败')
    } finally {
      setSending(false)
    }
  }

  function handleComposerKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter' || event.shiftKey) return
    event.preventDefault()
    if (
      sending ||
      !isWaitingForUser ||
      composerMode !== 'assistant_message' ||
      !composer.trim()
    ) {
      return
    }
    void handleDraft()
  }

  return {
    abortPopoverConversationId,
    abortReason,
    abortingConversationId,
    auth,
    availableToolSchemas,
    booting,
    chatScrollRef,
    composer,
    composerMode,
    thinkingText,
    conversations,
    deletingConversationId,
    draftBuffer,
    drawerOpen,
    handleAbortConversation,
    handleComposerKeyDown,
    handleCreateAutomationRule: automation.handleCreateAutomationRule,
    handleDeleteAutomationRule: automation.handleDeleteAutomationRule,
    handleDeleteConversation,
    handleDraft,
    handleEditAutomationRule: automation.handleEditAutomationRule,
    handleLogin,
    handleLogout,
    handlePruneConversations,
    handleSaveAutomationRule: automation.handleSaveAutomationRule,
    handleSelectConversation,
    handleSend,
    handleToggleAutomationRule: automation.handleToggleAutomationRule,
    handleTotpRefresh,
    isWaitingForUser,
    keyboardOffset,
    loginLoading,
    messages,
    messagesLoading,
    pruneKeepCount,
    pruneModalOpen,
    pruningConversations,
    automationRuleEditorOpen: automation.automationRuleEditorOpen,
    automationRules: automation.automationRules,
    automationRulesModalOpen: automation.automationRulesModalOpen,
    selectedConversation,
    selectedConversationId,
    selectedToolSchema,
    sending,
    setAbortPopoverConversationId,
    setAbortReason,
    setComposer,
    setComposerMode,
    setThinkingText,
    setDrawerOpen,
    setEditingAutomationRule: automation.setEditingAutomationRule,
    setPruneKeepCount,
    setPruneModalOpen,
    setAutomationRuleEditorOpen: automation.setAutomationRuleEditorOpen,
    setAutomationRules: automation.setAutomationRules,
    setAutomationRulesModalOpen: automation.setAutomationRulesModalOpen,
    setToolCallId,
    setToolFormValues,
    setToolName,
    editingAutomationRule: automation.editingAutomationRule,
    savingAutomationRules: automation.savingAutomationRules,
    totpEnabled,
    toolCallId,
    toolFormValues,
    toolName,
    visibleMessages,
  }
}
