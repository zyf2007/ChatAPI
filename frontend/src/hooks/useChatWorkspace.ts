import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'

import { requestFormJson, requestJson } from '../lib/api'
import { appMessage } from '../lib/antdMessage'
import {
  buildInitialToolFormValues,
  getLastBuiltinTools,
  getLastToolSchemas,
  normalizeChatText,
} from '../lib/chat-format'
import { buildVisibleTimeline } from '../lib/visibleTimeline'
import { buildToolCallPayload } from './chatWorkspace/buildToolCallPayload'
import { DEFAULT_AUTH_SESSION } from './chatWorkspace/defaultAuthSession'
import { useAutomationRules } from './useAutomationRules'
import { useKeyboardOffset } from './useKeyboardOffset'
import { useWorkspaceRealtime } from './useWorkspaceRealtime'
import type {
  AuthSession,
  AuthUser,
  ComposerMode,
  Conversation,
  TimelineItem,
  ReasoningStreamMode,
  ToolFieldValue,
  MessageItem,
  OutputImageAsset,
} from '../types/chat'

function isResponseOpenStatus(status: string | undefined) {
  return status === 'waiting' || status === 'streaming'
}

export function useChatWorkspace(isMobile: boolean) {
  const [booting, setBooting] = useState(true)
  const [auth, setAuth] = useState<AuthSession>(DEFAULT_AUTH_SESSION)
  const [loginLoading, setLoginLoading] = useState(false)
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedConversationId, setSelectedConversationId] = useState('')
  const [timelineByConversation, setTimelineByConversation] = useState<Record<string, TimelineItem[]>>({})
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [composer, setComposer] = useState('')
  const [thinkingText, setThinkingText] = useState('')
  const [composerMode, setComposerMode] = useState<ComposerMode>('assistant_message')
  const [reasoningStreamMode, setReasoningStreamMode] =
    useState<ReasoningStreamMode>('summery')
  const [toolName, setToolName] = useState('')
  const [toolCallId, setToolCallId] = useState('')
  const [toolFormValues, setToolFormValues] = useState<Record<string, ToolFieldValue>>({})
  const [builtinToolKind, setBuiltinToolKind] = useState('')
  const [builtinToolQuery, setBuiltinToolQuery] = useState('')
  const [builtinToolAsset, setBuiltinToolAsset] = useState<OutputImageAsset | null>(null)
  const [uploadingOutputImage, setUploadingOutputImage] = useState(false)
  const [sending, setSending] = useState(false)
  const sendingRef = useRef(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [deletingConversationId, setDeletingConversationId] = useState('')
  const [pruneModalOpen, setPruneModalOpen] = useState(false)
  const [pruneKeepCount, setPruneKeepCount] = useState<number>(20)
  const [pruningConversations, setPruningConversations] = useState(false)
  const [abortingConversationId, setAbortingConversationId] = useState('')
  const [abortPopoverConversationId, setAbortPopoverConversationId] = useState('')
  const [abortReason, setAbortReason] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)

  function beginSending() {
    if (sendingRef.current) return false
    sendingRef.current = true
    setSending(true)
    return true
  }

  function finishSending() {
    sendingRef.current = false
    setSending(false)
  }
  const chatScrollRef = useRef<HTMLDivElement | null>(null)
  const selectedConversationIdRef = useRef('')
  selectedConversationIdRef.current = selectedConversationId
  const selectedRealtimeStatusRef = useRef<{ conversationId: string; requestId: string; status: string }>({
    conversationId: '',
    requestId: '',
    status: '',
  })
  const keyboardOffset = useKeyboardOffset()
  const automation = useAutomationRules()
  const { loadAutomationRules, openRecordedDraft } = automation

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

  const {
    applySelectedConversation,
    automationExecutions,
    automationRecording,
    conversationPageError,
    hasMoreConversations,
    loadingMoreConversations,
    loadMoreConversations,
    sendAutomationRecordCommand,
    sendWorkspaceCommand,
  } = useWorkspaceRealtime({
    authenticated: auth.authenticated,
    conversations,
    onConnectionCountChange: handleConnectionCountChange,
    selectedConversationId,
    setConversations,
    setTimelineByConversation,
    setMessagesLoading,
    setSelectedConversationId,
  })

	const selectedConversation = conversations.find(
		(item) => item.id === selectedConversationId,
	)
	const selectedRequestId = selectedConversation?.request_id || ''
	const selectedExecution = automationExecutions[selectedConversationId]
	const automationExecution = selectedExecution?.request_id === selectedRequestId ? selectedExecution : null
  const timeline = timelineByConversation[selectedConversationId] ?? []
  const messages = timeline
    .filter((item): item is TimelineItem & { message: MessageItem } => item.kind === 'message' && !!item.message)
    .map((item) => item.message)
  const draftBuffer = selectedConversation?.draft_text ?? ''
  const isWaitingForUser = isResponseOpenStatus(selectedConversation?.status)
  const selectedConversationOpenRef = useRef(false)
  selectedConversationOpenRef.current = isWaitingForUser
  const selectedRequestIdRef = useRef('')
  selectedRequestIdRef.current = selectedConversation?.request_id ?? ''
  const selectedRequestFormat = selectedConversation?.request_format || ''
  const isResponsesConversation = selectedRequestFormat === 'responses'
  const availableToolSchemas = getLastToolSchemas(messages)
  const availableBuiltinTools = getLastBuiltinTools(messages)
  const selectedToolSchema =
    availableToolSchemas.find((item) => item.name === toolName) ?? null
  const visibleMessages = buildVisibleTimeline(timeline, draftBuffer)
  const openedRecordedRuleRef = useRef('')

  useEffect(() => {
    const draft = automationRecording.draft_rule
    if (!draft || draft.id === openedRecordedRuleRef.current) return
    openedRecordedRuleRef.current = draft.id
    openRecordedDraft(draft)
  }, [automationRecording.draft_rule, openRecordedDraft])

  useEffect(() => {
    const currentStatus = String(selectedConversation?.status || '')
    const currentRequestId = String(selectedConversation?.request_id || '')
    const previous = selectedRealtimeStatusRef.current
    if (
      selectedConversationId
      && previous.conversationId === selectedConversationId
      && (
        (previous.requestId && currentRequestId && previous.requestId !== currentRequestId) ||
        (isResponseOpenStatus(previous.status) && (currentStatus === 'aborted' || currentStatus === 'closed'))
      )
    ) {
      setComposer('')
      clearThinkingInput()
      setToolName('')
      setToolCallId('')
      setToolFormValues({})
      setBuiltinToolKind('')
      setBuiltinToolQuery('')
      setBuiltinToolAsset(null)
      setComposerMode('assistant_message')
    }
    selectedRealtimeStatusRef.current = {
      conversationId: selectedConversationId,
      requestId: currentRequestId,
      status: currentStatus,
    }
  }, [selectedConversationId, selectedConversation?.request_id, selectedConversation?.status])

  function clearThinkingInput() {
    setThinkingText('')
  }

  function normalizedOutputText(text: string) {
    return normalizeChatText(text)
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
          await loadAutomationRules()
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
  }, [loadAutomationRules])

  function selectComposerMode(nextMode: ComposerMode) {
    setComposerMode(nextMode)
    if (nextMode === 'tool_call' && !selectedToolSchema && availableToolSchemas[0]) {
      const schema = availableToolSchemas[0]
      setToolName(schema.name)
      setToolFormValues(buildInitialToolFormValues(schema.parameters))
    }
    if (
      nextMode === 'builtin_tool'
      && !availableBuiltinTools.some((item) => item.kind === builtinToolKind)
    ) {
      setBuiltinToolKind(availableBuiltinTools[0]?.kind ?? '')
    }
  }

  function selectToolName(nextToolName: string) {
    setToolName(nextToolName)
    const schema = availableToolSchemas.find((item) => item.name === nextToolName)
    setToolFormValues(buildInitialToolFormValues(schema?.parameters))
  }

  async function handleLogin(values: { username: string; password: string; totp?: string }) {
    setLoginLoading(true)
    try {
      await requestJson<{ ok: boolean; user: AuthUser }>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify(values),
      })
      const session = await requestJson<AuthSession>('/api/auth/session')
      setAuth(session)
      await loadAutomationRules()
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
      setTimelineByConversation({})
      setMessagesLoading(false)
      setComposer('')
      clearThinkingInput()
      setComposerMode('assistant_message')
      setReasoningStreamMode('summery')
      setToolName('')
      setToolCallId('')
      setToolFormValues({})
      setBuiltinToolKind('')
      setBuiltinToolQuery('')
      setBuiltinToolAsset(null)
      automation.resetAutomationRuleUi()
      automation.setAutomationRules([])
      localStorage.removeItem('chatapi.conversationId')
      appMessage.info('已退出登录')
      window.location.replace('/')
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
    setBuiltinToolKind('')
    setBuiltinToolQuery('')
    setBuiltinToolAsset(null)
  }

  async function handleDeleteConversation(conversationId: string) {
    const targetConversation = conversations.find((item) => item.id === conversationId)
    if (isResponseOpenStatus(targetConversation?.status)) {
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
      await sendWorkspaceCommand({
        kind: 'abort',
        conversation_id: conversationId,
        request_id: conversations.find((item) => item.id === conversationId)?.request_id || '',
        error: reason,
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

  async function handleAutomationRecording(action: 'start' | 'stop' | 'cancel') {
    if (!selectedConversationId) return
    try {
      const result = await sendAutomationRecordCommand(action, selectedConversationId)
      if (result.state.draft_rule) openRecordedDraft(result.state.draft_rule)
      appMessage.success(
        action === 'start' ? '已开始录制操作' : action === 'stop' ? '录制已生成规则草稿' : '已取消录制',
      )
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '录制操作失败')
    }
  }

  async function handleDraft(textOverride?: string) {
    if (!isWaitingForUser) return
    if (composerMode === 'tool_call') {
      await handleSend({ resetMode: true, successMessage: '已输出 Tool Call' })
      return
    }
    if (composerMode === 'builtin_tool') {
      const kind = builtinToolKind.trim()
      if (!kind) return
      if (kind === 'web_search' && !builtinToolQuery.trim()) {
        appMessage.warning('请输入搜索词')
        return
      }
      if (kind === 'image_generation' && !builtinToolAsset) {
        appMessage.warning('请上传图片')
        return
      }
      if (!beginSending()) return
      try {
        await sendWorkspaceCommand({
          kind: 'builtin_tool',
            conversation_id: selectedConversationId,
            request_id: selectedRequestId,
          builtin_tool_kind: kind,
          builtin_tool_query: kind === 'web_search' ? builtinToolQuery.trim() : undefined,
          builtin_tool_asset_id: kind === 'image_generation' ? builtinToolAsset?.asset_id : undefined,
        })
        setBuiltinToolQuery('')
        setBuiltinToolAsset(null)
        appMessage.success(kind === 'web_search' ? '已输出搜索事件' : '已输出生图事件')
      } catch (error) {
        appMessage.error(error instanceof Error ? error.message : '输出内置工具失败')
      } finally {
        finishSending()
      }
      return
    }
    const isThinkingMode = composerMode === 'thinking'
    const rawChunk = textOverride ?? (isThinkingMode ? thinkingText : composer)
    if (!normalizeChatText(rawChunk)) return
    const chunk = normalizedOutputText(rawChunk)
    if (!beginSending()) return
    try {
      const ack = await sendWorkspaceCommand({
        kind: 'stream_delta',
        conversation_id: selectedConversationId,
        request_id: selectedRequestId,
        text: chunk,
        mode: isThinkingMode ? 'thinking' : 'answer',
        reasoning_stream_mode:
          isThinkingMode && isResponsesConversation ? reasoningStreamMode : undefined,
      })
      if (isThinkingMode) {
        clearThinkingInput()
      } else {
        setComposer('')
      }
      appMessage.success(
        ack.auto_completed
          ? '已达到输出限制并自动结束'
          : isThinkingMode
            ? '已输出思考'
            : '已输出片段',
      )
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '输出片段失败')
    } finally {
      finishSending()
    }
  }

  async function handleOutputImageUpload(file: File) {
    const conversationId = selectedConversationId
    const requestId = selectedConversation?.request_id ?? ''
    setUploadingOutputImage(true)
    try {
      const form = new FormData()
      form.append('image', file)
      const asset = await requestFormJson<OutputImageAsset>(
        `/api/conversations/${encodeURIComponent(conversationId)}/output-images`,
        form,
      )
      if (
        selectedConversationIdRef.current !== conversationId ||
        selectedRequestIdRef.current !== requestId ||
        asset.conversation_id !== conversationId ||
        asset.request_id !== requestId ||
        !selectedConversationOpenRef.current
      ) {
        throw new Error('当前请求已变化，请重新上传图片')
      }
      setBuiltinToolAsset(asset)
      return asset
    } finally {
      setUploadingOutputImage(false)
    }
  }

  async function handleSend(options?: {
    resetMode?: boolean
    successMessage?: string
  }) {
    if (!isWaitingForUser) return
    const finalText =
      composerMode === 'tool_call'
        ? (() => {
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
        : composerMode === 'thinking'
          ? thinkingText
          : ''
    const pendingChunk = composerMode === 'assistant_message' ? composer : ''

    if (composerMode === 'assistant_message' && !draftBuffer && !normalizeChatText(pendingChunk)) {
      return
    }

    if (composerMode === 'tool_call' && !finalText) {
      return
    }
    if (composerMode === 'builtin_tool') {
      await handleDraft()
      return
    }

    if (!beginSending()) return
    try {
      if (composerMode === 'assistant_message' && pendingChunk) {
        const outputChunk = normalizedOutputText(pendingChunk)
        const deltaAck = await sendWorkspaceCommand({
          kind: 'stream_delta',
            conversation_id: selectedConversationId,
            request_id: selectedRequestId,
          text: outputChunk,
          mode: 'answer',
        })
        setComposer('')
        if (deltaAck.auto_completed) {
          appMessage.success('已达到输出限制并自动结束')
          return
        }
      } else if (composerMode === 'thinking' && finalText) {
        const outputChunk = normalizedOutputText(finalText)
        const deltaAck = await sendWorkspaceCommand({
          kind: 'stream_delta',
            conversation_id: selectedConversationId,
            request_id: selectedRequestId,
          text: outputChunk,
          mode: 'thinking',
          reasoning_stream_mode: isResponsesConversation ? reasoningStreamMode : undefined,
        })
        clearThinkingInput()
        if (deltaAck.auto_completed) {
          appMessage.success('已达到输出限制并自动结束')
          return
        }
      }

      await sendWorkspaceCommand({
        kind: 'stream_complete',
        conversation_id: selectedConversationId,
        request_id: selectedRequestId,
        text: composerMode === 'tool_call' ? finalText : undefined,
        mode: composerMode,
        tool_name: composerMode === 'tool_call' ? toolName.trim() || undefined : undefined,
        tool_call_id: composerMode === 'tool_call' ? toolCallId.trim() || undefined : undefined,
        reasoning_stream_mode:
          composerMode === 'thinking' && isResponsesConversation ? reasoningStreamMode : undefined,
      })
      setComposer('')
      clearThinkingInput()
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
      finishSending()
    }
  }

  function handleComposerKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter') return

    const isAnswerMode = composerMode === 'assistant_message'
    const isThinkingMode = composerMode === 'thinking'
    const canStreamChunk = isAnswerMode || isThinkingMode

    if (event.altKey) {
      event.preventDefault()
      // Only answer mode reuses the streamed draft buffer back into the editor.
      if (sending || !isWaitingForUser || !isAnswerMode || !draftBuffer) {
        return
      }
      setComposer(`${draftBuffer}${composer}`)
      return
    }

    if (event.ctrlKey || event.metaKey) {
      event.preventDefault()
      // Ending a turn is only available from answer mode.
      if (sending || !isWaitingForUser || !isAnswerMode) {
        return
      }
      void handleSend()
      return
    }

    if (event.shiftKey) return

    // Enter = stream current chunk (answer or thinking), same as clicking the stream button.
    event.preventDefault()
    const textarea = event.currentTarget
    if (sending || !isWaitingForUser || !canStreamChunk || !normalizeChatText(textarea.value)) {
      return
    }
    void handleDraft(textarea.value).finally(() => {
      window.requestAnimationFrame(() => textarea.focus())
    })
  }

  return {
    abortPopoverConversationId,
    abortReason,
    abortingConversationId,
    auth,
    availableToolSchemas,
    availableBuiltinTools,
    booting,
    chatScrollRef,
    composer,
    composerMode,
    builtinToolKind,
    builtinToolQuery,
    builtinToolAsset,
    uploadingOutputImage,
    thinkingText,
    conversations,
    conversationPageError,
    deletingConversationId,
    draftBuffer,
    drawerOpen,
    handleAbortConversation,
    handleAutomationRecording,
    handleComposerKeyDown,
    handleCreateAutomationRule: automation.handleCreateAutomationRule,
    handleDeleteAutomationRule: automation.handleDeleteAutomationRule,
    handleDeleteConversation,
    handleDraft,
    handleOutputImageUpload,
    handleEditAutomationRule: automation.handleEditAutomationRule,
    handleLogin,
    handleLogout,
    hasMoreConversations,
    loadingMoreConversations,
    loadMoreConversations,
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
    automationExecution,
    automationRecording,
    automationRules: automation.automationRules,
    automationRulesModalOpen: automation.automationRulesModalOpen,
    selectedConversation,
    selectedConversationId,
    selectedRequestFormat,
    selectedToolSchema,
    isResponsesConversation,
    reasoningStreamMode,
    sending,
    setAbortPopoverConversationId,
    setAbortReason,
    setComposer,
    setComposerMode: selectComposerMode,
    setBuiltinToolKind,
    setBuiltinToolQuery,
    setBuiltinToolAsset,
    setThinkingText,
    setReasoningStreamMode,
    setDrawerOpen,
    setEditingAutomationRule: automation.setEditingAutomationRule,
    setPruneKeepCount,
    setPruneModalOpen,
    setAutomationRuleEditorOpen: automation.setAutomationRuleEditorOpen,
    setAutomationRules: automation.setAutomationRules,
    setAutomationRulesModalOpen: automation.setAutomationRulesModalOpen,
    setToolCallId,
    setToolFormValues,
    setToolName: selectToolName,
    editingAutomationRule: automation.editingAutomationRule,
    savingAutomationRules: automation.savingAutomationRules,
    totpEnabled,
    toolCallId,
    toolFormValues,
    toolName,
    visibleMessages,
  }
}
