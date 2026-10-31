import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'

import { resolveWebSocketUrl } from '../lib/api'
import type {
  AutomationExecutionState,
  AutomationExecutionStateEvent,
  AutomationRecordAckEvent,
  AutomationRecordErrorEvent,
  AutomationRecordingState,
  AutomationRecordStateEvent,
  Conversation,
  TimelineItem,
  WorkspaceCommand,
  WorkspaceCommandAckEvent,
  WorkspaceCommandErrorEvent,
  WorkspaceConnectionCountEvent,
  WorkspaceConversationDeleteEvent,
  WorkspaceConversationPageErrorEvent,
  WorkspaceConversationPageEvent,
  WorkspaceConversationUpsertEvent,
  WorkspaceSnapshotEvent,
  WorkspaceTimelineItemAppendEvent,
  WorkspaceTimelineResetEvent,
} from '../types/chat'

const STORAGE_KEY = 'chatapi.conversationId'

function normalizeAutomationRecordingState(state: AutomationRecordingState): AutomationRecordingState {
  return {
    ...state,
    steps: Array.isArray(state.steps) ? state.steps : [],
    draft_rule: state.draft_rule
      ? {
          ...state.draft_rule,
          steps: Array.isArray(state.draft_rule.steps) ? state.draft_rule.steps : [],
        }
      : undefined,
  }
}

function sortConversations(items: Conversation[]) {
  return [...items].sort((left, right) => {
    const timeOrder = Date.parse(right.updated_at) - Date.parse(left.updated_at)
    return timeOrder || right.id.localeCompare(left.id)
  })
}

type UseWorkspaceRealtimeParams = {
  authenticated: boolean
  automationEnabled: boolean
  conversations: Conversation[]
  onConnectionCountChange: (value: number) => void
  onConversationCountChange: (value: number) => void
  selectedConversationId: string
  setConversations: Dispatch<SetStateAction<Conversation[]>>
  setTimelineByConversation: Dispatch<SetStateAction<Record<string, TimelineItem[]>>>
  setMessagesLoading: Dispatch<SetStateAction<boolean>>
  setSelectedConversationId: Dispatch<SetStateAction<string>>
}

export function useWorkspaceRealtime({
  authenticated,
  automationEnabled,
  conversations,
  onConnectionCountChange,
  onConversationCountChange,
  selectedConversationId,
  setConversations,
  setTimelineByConversation,
  setMessagesLoading,
  setSelectedConversationId,
}: UseWorkspaceRealtimeParams) {
  const conversationsRef = useRef<Conversation[]>([])
  const selectedConversationIdRef = useRef('')
  const socketRef = useRef<WebSocket | null>(null)
  const subscribedConversationIdRef = useRef('')
  const commandSeqRef = useRef(0)
  const automationSnapshotRevisionRef = useRef(0)
  const automationRecordingRevisionRef = useRef(0)
  const automationExecutionRevisionsRef = useRef<Record<string, number>>({})
  const conversationCursorRef = useRef('')
  const conversationPageCommandRef = useRef('')
  const conversationPageTimeoutRef = useRef(0)
  const removedConversationIDsRef = useRef(new Set<string>())
  const [hasMoreConversations, setHasMoreConversations] = useState(false)
  const [loadingMoreConversations, setLoadingMoreConversations] = useState(false)
  const [conversationPageError, setConversationPageError] = useState('')
  const [automationRecording, setAutomationRecording] = useState<AutomationRecordingState>({ revision: 0, active: false, steps: [] })
  const [automationExecutions, setAutomationExecutions] = useState<Record<string, AutomationExecutionState>>({})
  const pendingCommandsRef = useRef(new Map<string, {
    reject: (error: Error) => void
    resolve: (ack: unknown) => void
    timeout: number
  expectedRequestId?: string
  }>())

  const resolvePreferredConversationId = useCallback((items: Conversation[]) => {
    const requested = selectedConversationIdRef.current || localStorage.getItem(STORAGE_KEY) || ''
    if (requested && items.some((item) => item.id === requested)) {
      return requested
    }
    return items[0]?.id ?? ''
  }, [])

  const sendJSON = useCallback((payload: unknown) => {
    if (socketRef.current?.readyState !== WebSocket.OPEN) return
    socketRef.current.send(JSON.stringify(payload))
  }, [])

  const loadMoreConversations = useCallback(() => {
    const socket = socketRef.current
    const cursor = conversationCursorRef.current
    if (socket?.readyState !== WebSocket.OPEN || !cursor || conversationPageCommandRef.current) return
    const commandId = `page_${Date.now()}_${(commandSeqRef.current += 1)}`
    conversationPageCommandRef.current = commandId
    setLoadingMoreConversations(true)
    setConversationPageError('')
    conversationPageTimeoutRef.current = window.setTimeout(() => {
      if (conversationPageCommandRef.current !== commandId) return
      conversationPageCommandRef.current = ''
      setLoadingMoreConversations(false)
      setConversationPageError('加载超时，请重试')
    }, 15_000)
    try {
      socket.send(JSON.stringify({ type: 'conversation.page.get', command_id: commandId, cursor }))
    } catch {
      conversationPageCommandRef.current = ''
      window.clearTimeout(conversationPageTimeoutRef.current)
      setLoadingMoreConversations(false)
      setConversationPageError('发送失败，请重试')
    }
  }, [])

  const rejectPendingCommands = useCallback((message: string) => {
    for (const pending of pendingCommandsRef.current.values()) {
      window.clearTimeout(pending.timeout)
      pending.reject(new Error(message))
    }
    pendingCommandsRef.current.clear()
  }, [])

  const sendWorkspaceCommand = useCallback((command: Omit<WorkspaceCommand, 'command_id'>) => {
    const commandId = `cmd_${Date.now()}_${(commandSeqRef.current += 1)}`
    const socket = socketRef.current
    if (socket?.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('实时连接尚未就绪'))
    }
    return new Promise<WorkspaceCommandAckEvent>((resolve, reject) => {
      const timeout = window.setTimeout(() => {
        pendingCommandsRef.current.delete(commandId)
        reject(new Error('等待服务端确认超时'))
      }, 15_000)
    pendingCommandsRef.current.set(commandId, {
      reject,
      resolve: (ack) => resolve(ack as WorkspaceCommandAckEvent),
      timeout,
      expectedRequestId: command.request_id,
    })
      try {
        socket.send(JSON.stringify({
          type: 'workspace.command',
          command: {
            command_id: commandId,
            ...command,
          },
        }))
      } catch (error) {
        window.clearTimeout(timeout)
        pendingCommandsRef.current.delete(commandId)
        reject(error instanceof Error ? error : new Error('发送实时命令失败'))
      }
    })
  }, [])

  const sendAutomationRecordCommand = useCallback((
    action: 'start' | 'stop' | 'cancel' | 'get',
    conversationId: string,
  ) => {
    const commandId = `automation_${Date.now()}_${(commandSeqRef.current += 1)}`
    const socket = socketRef.current
    if (socket?.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('实时连接尚未就绪'))
    }
    return new Promise<AutomationRecordAckEvent>((resolve, reject) => {
      const timeout = window.setTimeout(() => {
        pendingCommandsRef.current.delete(commandId)
        reject(new Error('等待服务端确认超时'))
      }, 15_000)
      pendingCommandsRef.current.set(commandId, {
        reject,
        resolve: (ack) => resolve(ack as AutomationRecordAckEvent),
        timeout,
      })
      try {
        socket.send(JSON.stringify({
          type: `automation.record.${action}`,
          command_id: commandId,
          conversation_id: conversationId,
        }))
      } catch (error) {
        window.clearTimeout(timeout)
        pendingCommandsRef.current.delete(commandId)
        reject(error instanceof Error ? error : new Error('发送录制命令失败'))
      }
    })
  }, [])

  const subscribeConversation = useCallback((conversationId: string) => {
    const nextID = conversationId.trim()
    const previousID = subscribedConversationIdRef.current
    if (previousID && previousID !== nextID) {
      sendJSON({ type: 'timeline.unsubscribe', conversation_id: previousID })
    }
    subscribedConversationIdRef.current = nextID
    if (nextID) {
      setMessagesLoading(true)
      sendJSON({ type: 'timeline.subscribe', conversation_id: nextID })
      return
    }
    setMessagesLoading(false)
  }, [sendJSON, setMessagesLoading])

  const applySelectedConversation = useCallback((nextConversationId: string) => {
    selectedConversationIdRef.current = nextConversationId
    setSelectedConversationId((current) => (current === nextConversationId ? current : nextConversationId))
    if (nextConversationId) {
      localStorage.setItem(STORAGE_KEY, nextConversationId)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
    subscribeConversation(nextConversationId)
  }, [setSelectedConversationId, subscribeConversation])

  useEffect(() => {
    conversationsRef.current = conversations
  }, [conversations])

  useEffect(() => {
    selectedConversationIdRef.current = selectedConversationId
  }, [selectedConversationId])

  useEffect(() => {
    if (!authenticated) return
    let active = true
    let reconnectTimer = 0
    const removedConversationIDs = removedConversationIDsRef.current

    function connect() {
      const socket = new WebSocket(resolveWebSocketUrl('/api/ws'))
      socketRef.current = socket
      subscribedConversationIdRef.current = ''
      setMessagesLoading(true)

      socket.addEventListener('open', () => {
        sendJSON({ type: 'workspace.ping' })
        if (automationEnabled) {
          void sendAutomationRecordCommand('get', selectedConversationIdRef.current).catch(() => undefined)
        }
      })

      socket.addEventListener('message', (event) => {
        if (!active) return
        let payload:
          | WorkspaceSnapshotEvent
          | WorkspaceConnectionCountEvent
          | WorkspaceConversationUpsertEvent
          | WorkspaceConversationDeleteEvent
          | WorkspaceConversationPageEvent
          | WorkspaceConversationPageErrorEvent
          | WorkspaceTimelineResetEvent
          | WorkspaceTimelineItemAppendEvent
          | WorkspaceCommandAckEvent
          | WorkspaceCommandErrorEvent
          | AutomationRecordAckEvent
          | AutomationRecordErrorEvent
          | AutomationRecordStateEvent
          | AutomationExecutionStateEvent
          | { type: 'disconnect'; reason?: string }
          | { type: 'workspace.ping' }
        try {
          payload = JSON.parse(event.data) as
            | WorkspaceSnapshotEvent
            | WorkspaceConnectionCountEvent
            | WorkspaceConversationUpsertEvent
            | WorkspaceConversationDeleteEvent
            | WorkspaceConversationPageEvent
            | WorkspaceConversationPageErrorEvent
            | WorkspaceTimelineResetEvent
            | WorkspaceTimelineItemAppendEvent
            | WorkspaceCommandAckEvent
            | WorkspaceCommandErrorEvent
            | AutomationRecordAckEvent
            | AutomationRecordErrorEvent
            | AutomationRecordStateEvent
            | AutomationExecutionStateEvent
            | { type: 'disconnect'; reason?: string }
            | { type: 'workspace.ping' }
        } catch {
          return
        }

        if (payload.type === 'workspace.ping') return
        if (payload.type === 'disconnect') {
          socket.close()
          return
        }

        if (payload.type === 'workspace.snapshot') {
          const nextConversations = sortConversations(payload.conversations)
          conversationCursorRef.current = payload.next_cursor ?? ''
          conversationPageCommandRef.current = ''
          removedConversationIDs.clear()
          window.clearTimeout(conversationPageTimeoutRef.current)
          setHasMoreConversations(payload.has_more)
          setLoadingMoreConversations(false)
          setConversationPageError('')
          onConversationCountChange(payload.conversation_count)
          conversationsRef.current = nextConversations
          setConversations(nextConversations)
          applySelectedConversation(resolvePreferredConversationId(nextConversations))
          return
        }

        if (payload.type === 'conversation.page') {
          if (payload.command_id !== conversationPageCommandRef.current) return
          conversationPageCommandRef.current = ''
          window.clearTimeout(conversationPageTimeoutRef.current)
          conversationCursorRef.current = payload.next_cursor ?? ''
          setHasMoreConversations(payload.has_more)
          setLoadingMoreConversations(false)
          const byID = new Map(conversationsRef.current.map((item) => [item.id, item]))
          for (const conversation of payload.conversations) {
            if (!byID.has(conversation.id) && !removedConversationIDs.has(conversation.id)) {
              byID.set(conversation.id, conversation)
            }
          }
          const nextConversations = sortConversations([...byID.values()])
          conversationsRef.current = nextConversations
          setConversations(nextConversations)
          return
        }

        if (payload.type === 'conversation.page.error') {
          if (payload.command_id !== conversationPageCommandRef.current) return
          conversationPageCommandRef.current = ''
          window.clearTimeout(conversationPageTimeoutRef.current)
          setLoadingMoreConversations(false)
          setConversationPageError(payload.message || '加载失败，请重试')
          return
        }

        if (payload.type === 'workspace.connections') {
          onConnectionCountChange(payload.current_connection_count)
          return
        }

        if (payload.type === 'workspace.command_ack') {
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
      if (pending.expectedRequestId && pending.expectedRequestId !== payload.request_id) {
        pending.reject(new Error('服务端确认的请求已发生变化'))
        return
      }
            pending.resolve(payload)
          }
          return
        }

        if (payload.type === 'workspace.command_error') {
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
            pending.reject(new Error(payload.message || payload.code))
          }
          return
        }

        if (payload.type === 'automation.record.ack') {
          const normalizedPayload = {
            ...payload,
            state: normalizeAutomationRecordingState(payload.state),
          }
          if (payload.revision >= automationSnapshotRevisionRef.current) {
            automationSnapshotRevisionRef.current = payload.revision
            if (normalizedPayload.state.revision >= automationRecordingRevisionRef.current) {
              automationRecordingRevisionRef.current = normalizedPayload.state.revision
              setAutomationRecording(normalizedPayload.state)
            }
            if (payload.executions) {
              setAutomationExecutions((current) => {
        const next = Object.fromEntries(
          payload.executions!
            .filter((item) => item.revision >= (automationExecutionRevisionsRef.current[item.conversation_id] ?? 0))
            .map((item) => [item.conversation_id, item]),
        )
                for (const [conversationId, item] of Object.entries(current)) {
                  const snapshotItem = next[conversationId]
                  if (item.revision > payload.revision || (snapshotItem && item.revision > snapshotItem.revision)) {
                    next[conversationId] = item
                  }
                }
        for (const [conversationId, item] of Object.entries(next)) {
          automationExecutionRevisionsRef.current[conversationId] = Math.max(
            automationExecutionRevisionsRef.current[conversationId] ?? 0,
            item.revision,
          )
        }
                return next
              })
            }
          }
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
            pending.resolve(normalizedPayload)
          }
          return
        }

        if (payload.type === 'automation.record.error') {
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
            pending.reject(new Error(payload.message || payload.code))
          }
          return
        }

        if (payload.type === 'automation.record.state') {
          const normalizedState = normalizeAutomationRecordingState(payload.state)
      if (normalizedState.revision < automationRecordingRevisionRef.current) return
      automationRecordingRevisionRef.current = normalizedState.revision
          setAutomationRecording(normalizedState)
          return
        }

        if (payload.type === 'automation.execution.state') {
          const conversationId = payload.execution.conversation_id
          if (payload.execution.revision < (automationExecutionRevisionsRef.current[conversationId] ?? 0)) return
          automationExecutionRevisionsRef.current[conversationId] = payload.execution.revision
      if (payload.execution.status === 'removed') {
        setAutomationExecutions((current) => {
          const next = { ...current }
          delete next[conversationId]
          return next
        })
        return
      }
          setAutomationExecutions((current) => ({
            ...current,
        [conversationId]: payload.execution,
          }))
          return
        }

        if (payload.type === 'conversation.upsert') {
          if (typeof payload.conversation_count === 'number') {
            onConversationCountChange(payload.conversation_count)
          }
          removedConversationIDs.delete(payload.conversation.id)
          const remaining = conversationsRef.current.filter((item) => item.id !== payload.conversation.id)
          const nextConversations = sortConversations([payload.conversation, ...remaining])
          conversationsRef.current = nextConversations
          setConversations(nextConversations)
          if (!selectedConversationIdRef.current) {
            applySelectedConversation(resolvePreferredConversationId(nextConversations))
          }
          return
        }

        if (payload.type === 'timeline.reset') {
          setTimelineByConversation((current) => ({
            ...current,
            [payload.conversation_id]: payload.items,
          }))
          setMessagesLoading(false)
          return
        }

        if (payload.type === 'timeline.append') {
          setTimelineByConversation((current) => {
            const existing = current[payload.conversation_id] ?? []
            if (existing.some((item) => item.id === payload.item.id)) {
              return current
            }
            return {
              ...current,
              [payload.conversation_id]: [...existing, payload.item],
            }
          })
          return
        }

        if (typeof payload.conversation_count === 'number') {
          onConversationCountChange(payload.conversation_count)
        }
        removedConversationIDs.add(payload.conversation_id)
        const nextConversations = conversationsRef.current.filter((item) => item.id !== payload.conversation_id)
        conversationsRef.current = nextConversations
        setConversations(nextConversations)
        setTimelineByConversation((current) => {
          if (!Object.prototype.hasOwnProperty.call(current, payload.conversation_id)) {
            return current
          }
          const next = { ...current }
          delete next[payload.conversation_id]
          return next
        })
        if (selectedConversationIdRef.current === payload.conversation_id) {
          applySelectedConversation(resolvePreferredConversationId(nextConversations))
        }
      })

      socket.addEventListener('close', () => {
        if (!active) return
        rejectPendingCommands('实时连接已断开')
    automationSnapshotRevisionRef.current = 0
    automationRecordingRevisionRef.current = 0
    automationExecutionRevisionsRef.current = {}
    setAutomationRecording({ revision: 0, active: false, steps: [] })
        setAutomationExecutions({})
        if (socketRef.current === socket) {
          socketRef.current = null
        }
        subscribedConversationIdRef.current = ''
        conversationCursorRef.current = ''
        conversationPageCommandRef.current = ''
        window.clearTimeout(conversationPageTimeoutRef.current)
        removedConversationIDs.clear()
        setHasMoreConversations(false)
        setLoadingMoreConversations(false)
        setConversationPageError('')
        setMessagesLoading(true)
        reconnectTimer = window.setTimeout(() => {
          connect()
        }, 1000)
      })

      socket.addEventListener('error', () => {
        socket.close()
      })
    }

    connect()

    return () => {
      active = false
      rejectPendingCommands('实时连接已关闭')
      window.clearTimeout(reconnectTimer)
      socketRef.current?.close()
      socketRef.current = null
      subscribedConversationIdRef.current = ''
      conversationCursorRef.current = ''
      conversationPageCommandRef.current = ''
      window.clearTimeout(conversationPageTimeoutRef.current)
      removedConversationIDs.clear()
    }
  }, [
    applySelectedConversation,
    authenticated,
    automationEnabled,
    onConnectionCountChange,
    onConversationCountChange,
    rejectPendingCommands,
    resolvePreferredConversationId,
    sendJSON,
    sendAutomationRecordCommand,
    setConversations,
    setMessagesLoading,
    setSelectedConversationId,
    setTimelineByConversation,
  ])

  return {
    applySelectedConversation,
    automationExecutions,
    automationRecording,
    conversationPageError,
    hasMoreConversations,
    loadingMoreConversations,
    loadMoreConversations,
    sendAutomationRecordCommand,
    sendWorkspaceCommand,
  }
}
