import { useEffect, useRef, type Dispatch, type SetStateAction } from 'react'

import { resolveWebSocketUrl } from '../lib/api'
import type {
  Conversation,
  MessageItem,
  WorkspaceConnectionCountEvent,
  WorkspaceConversationDeleteEvent,
  WorkspaceConversationUpsertEvent,
  WorkspaceSnapshotEvent,
} from '../types/chat'

const STORAGE_KEY = 'chatapi.conversationId'

function sortConversations(items: Conversation[]) {
  return [...items].sort((left, right) => {
    return Date.parse(right.updated_at) - Date.parse(left.updated_at)
  })
}

type UseWorkspaceRealtimeParams = {
  authenticated: boolean
  conversations: Conversation[]
  onConnectionCountChange: (value: number) => void
  selectedConversationId: string
  setConversations: Dispatch<SetStateAction<Conversation[]>>
  setDraftBuffers: Dispatch<SetStateAction<Record<string, string>>>
  setMessagesByConversation: Dispatch<SetStateAction<Record<string, MessageItem[]>>>
  setLoadedConversationIds: Dispatch<SetStateAction<Set<string>>>
  setMessagesLoading: Dispatch<SetStateAction<boolean>>
  setSelectedConversationId: Dispatch<SetStateAction<string>>
}

export function useWorkspaceRealtime({
  authenticated,
  conversations,
  onConnectionCountChange,
  selectedConversationId,
  setConversations,
  setDraftBuffers,
  setMessagesByConversation,
  setLoadedConversationIds,
  setMessagesLoading,
  setSelectedConversationId,
}: UseWorkspaceRealtimeParams) {
  const conversationsRef = useRef<Conversation[]>([])
  const selectedConversationIdRef = useRef('')
  const socketRef = useRef<WebSocket | null>(null)

  function resolvePreferredConversationId(items: Conversation[]) {
    const requested =
      selectedConversationIdRef.current || localStorage.getItem(STORAGE_KEY) || ''
    if (requested && items.some((item) => item.id === requested)) {
      return requested
    }
    return items[0]?.id ?? ''
  }

  function applySelectedConversation(nextConversationId: string) {
    selectedConversationIdRef.current = nextConversationId
    setSelectedConversationId((current) =>
      current === nextConversationId ? current : nextConversationId,
    )
    if (nextConversationId) {
      localStorage.setItem(STORAGE_KEY, nextConversationId)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  }

  useEffect(() => {
    conversationsRef.current = conversations
  }, [conversations])

  useEffect(() => {
    selectedConversationIdRef.current = selectedConversationId
  }, [selectedConversationId])

  useEffect(() => {
    setDraftBuffers((prev) => {
      let changed = false
      const next = { ...prev }
      for (const conversation of conversations) {
        const draftText = conversation.metadata?.realtime_draft_text
        if (typeof draftText !== 'string') continue
        if (draftText) {
          if (next[conversation.id] !== draftText) {
            next[conversation.id] = draftText
            changed = true
          }
        } else if (next[conversation.id]) {
          delete next[conversation.id]
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [conversations, setDraftBuffers])

  useEffect(() => {
    if (!authenticated) return
    let active = true
    let reconnectTimer = 0

    function connect() {
      const socket = new WebSocket(resolveWebSocketUrl('/api/ws'))
      socketRef.current = socket
      setMessagesLoading(true)

      socket.addEventListener('message', (event) => {
        if (!active) return
        let payload:
          | WorkspaceSnapshotEvent
          | WorkspaceConnectionCountEvent
          | WorkspaceConversationUpsertEvent
          | WorkspaceConversationDeleteEvent
          | { type: 'disconnect'; reason?: string }
          | { type: 'ping' }
        try {
          payload = JSON.parse(event.data) as
            | WorkspaceSnapshotEvent
            | WorkspaceConnectionCountEvent
            | WorkspaceConversationUpsertEvent
            | WorkspaceConversationDeleteEvent
            | { type: 'disconnect'; reason?: string }
            | { type: 'ping' }
        } catch {
          return
        }
        if (payload.type === 'ping') {
          return
        }
        if (payload.type === 'disconnect') {
          socket.close()
          return
        }

        if (payload.type === 'snapshot') {
          const nextConversations = sortConversations(payload.conversations)
          setConversations(nextConversations)
          applySelectedConversation(resolvePreferredConversationId(nextConversations))
          return
        }

        if (payload.type === 'connection_count') {
          onConnectionCountChange(payload.current_connection_count)
          return
        }

        if (payload.type === 'conversation_upsert') {
          const remaining = conversationsRef.current.filter(
            (item) => item.id !== payload.conversation.id,
          )
          const nextConversations = sortConversations([
            payload.conversation,
            ...remaining,
          ])
          conversationsRef.current = nextConversations
          setConversations(nextConversations)
          applySelectedConversation(resolvePreferredConversationId(nextConversations))
          if (payload.messages) {
            setMessagesByConversation((current) => ({
              ...current,
              [payload.conversation.id]: payload.messages ?? [],
            }))
            setLoadedConversationIds((current) => {
              const next = new Set(current)
              next.add(payload.conversation.id)
              return next
            })
          } else {
            setLoadedConversationIds((current) => {
              if (!current.has(payload.conversation.id)) return current
              const next = new Set(current)
              next.delete(payload.conversation.id)
              return next
            })
          }
          return
        }

        const nextConversations = conversationsRef.current.filter(
          (item) => item.id !== payload.conversation_id,
        )
        conversationsRef.current = nextConversations
        setConversations(nextConversations)
        applySelectedConversation(resolvePreferredConversationId(nextConversations))
        setMessagesByConversation((current) => {
          if (!Object.prototype.hasOwnProperty.call(current, payload.conversation_id)) {
            return current
          }
          const next = { ...current }
          delete next[payload.conversation_id]
          return next
        })
      })

      socket.addEventListener('close', () => {
        if (!active) return
        if (socketRef.current === socket) {
          socketRef.current = null
        }
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
      window.clearTimeout(reconnectTimer)
      socketRef.current?.close()
      socketRef.current = null
    }
  }, [
    authenticated,
    onConnectionCountChange,
    setConversations,
    setMessagesByConversation,
    setMessagesLoading,
  ])

  return {
    applySelectedConversation,
  }
}
