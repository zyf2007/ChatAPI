import { useEffect, useRef, useState } from 'react'
import { Drawer, Grid, Layout, Spin } from 'antd'
import { Navigate } from 'react-router-dom'

import { ChatPane } from './ChatPane'
import { ConversationSidebar } from './ConversationSidebar'
import { useChatWorkspace } from '../hooks/useChatWorkspace'

const { Header, Sider, Content } = Layout
const SIDEBAR_COLLAPSED_KEY = 'chatapi.sidebar.collapsed'
const SIDEBAR_WIDTH_KEY = 'chatapi.sidebar.width'

export function WorkspaceRoute() {
  const screens = Grid.useBreakpoint()
  const isMobile = !screens.md
  const workspace = useChatWorkspace(isMobile)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    if (typeof window === 'undefined') return false
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1'
  })
  const [sidebarWidth, setSidebarWidth] = useState(() => {
    if (typeof window === 'undefined') return 320
    const raw = window.localStorage.getItem(SIDEBAR_WIDTH_KEY)
    if (!raw) return 320
    const parsed = Number(raw)
    if (!Number.isFinite(parsed)) return 320
    return Math.min(480, Math.max(220, parsed))
  })
  const sidebarElementRef = useRef<HTMLDivElement | null>(null)
  const sidebarResizeRef = useRef<{ startX: number; startWidth: number } | null>(null)
  const liveSidebarWidthRef = useRef(sidebarWidth)

  function applySidebarWidth(width: number) {
    const sidebarElement = sidebarElementRef.current
    if (!sidebarElement) return
    const nextWidth = `${width}px`
    sidebarElement.style.width = nextWidth
    sidebarElement.style.minWidth = nextWidth
    sidebarElement.style.maxWidth = nextWidth
    sidebarElement.style.flex = `0 0 ${nextWidth}`
  }

  useEffect(() => {
    liveSidebarWidthRef.current = sidebarWidth
  }, [sidebarWidth])

  useEffect(() => {
    if (isMobile) return

    function handlePointerMove(event: PointerEvent) {
      const current = sidebarResizeRef.current
      if (!current) return
      const nextWidth = Math.min(480, Math.max(220, current.startWidth + event.clientX - current.startX))
      liveSidebarWidthRef.current = nextWidth
      applySidebarWidth(nextWidth)
    }

    function handlePointerUp() {
      const current = sidebarResizeRef.current
      if (current) {
        setSidebarWidth((previous) => {
          const nextWidth = liveSidebarWidthRef.current
          return previous === nextWidth ? previous : nextWidth
        })
      }
      sidebarResizeRef.current = null
      document.body.classList.remove('is-resizing-sidebar')
    }

    window.addEventListener('pointermove', handlePointerMove)
    window.addEventListener('pointerup', handlePointerUp)
    return () => {
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('pointerup', handlePointerUp)
    }
  }, [isMobile])

  useEffect(() => {
    if (typeof window === 'undefined') return
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, sidebarCollapsed ? '1' : '0')
  }, [sidebarCollapsed])

  useEffect(() => {
    if (typeof window === 'undefined') return
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth))
  }, [sidebarWidth])

  useEffect(() => {
    if (isMobile) return
    applySidebarWidth(sidebarCollapsed ? 72 : sidebarWidth)
  }, [isMobile, sidebarCollapsed, sidebarWidth])

  if (workspace.booting) {
    return (
      <div className="boot-screen">
        <Spin size="large" />
      </div>
    )
  }

  const sidebar = (
    <ConversationSidebar
      abortPopoverConversationId={workspace.abortPopoverConversationId}
      abortReason={workspace.abortReason}
      abortingConversationId={workspace.abortingConversationId}
      auth={workspace.auth}
      automationRuleEditorOpen={workspace.automationRuleEditorOpen}
      automationRules={workspace.automationRules}
      automationRulesModalOpen={workspace.automationRulesModalOpen}
      collapsed={!isMobile && sidebarCollapsed}
      conversations={workspace.conversations}
      deletingConversationId={workspace.deletingConversationId}
      editingAutomationRule={workspace.editingAutomationRule}
      onAbortConversation={workspace.handleAbortConversation}
      onCreateAutomationRule={workspace.handleCreateAutomationRule}
      onDeleteAutomationRule={workspace.handleDeleteAutomationRule}
      onDeleteConversation={workspace.handleDeleteConversation}
      onEditAutomationRule={workspace.handleEditAutomationRule}
      onLogout={workspace.handleLogout}
      onPruneConversations={workspace.handlePruneConversations}
      onSaveAutomationRule={workspace.handleSaveAutomationRule}
      onSelectConversation={workspace.handleSelectConversation}
      onToggleAutomationRule={workspace.handleToggleAutomationRule}
      onToggleCollapsed={() => setSidebarCollapsed((value) => !value)}
      onTotpRefresh={workspace.handleTotpRefresh}
      pruneKeepCount={workspace.pruneKeepCount}
      pruneModalOpen={workspace.pruneModalOpen}
      pruningConversations={workspace.pruningConversations}
      savingAutomationRules={workspace.savingAutomationRules}
      selectedConversationId={workspace.selectedConversationId}
      setAbortPopoverConversationId={workspace.setAbortPopoverConversationId}
      setAbortReason={workspace.setAbortReason}
      setAutomationRuleEditorOpen={workspace.setAutomationRuleEditorOpen}
      setAutomationRulesModalOpen={workspace.setAutomationRulesModalOpen}
      setEditingAutomationRule={workspace.setEditingAutomationRule}
      setPruneKeepCount={workspace.setPruneKeepCount}
      setPruneModalOpen={workspace.setPruneModalOpen}
      totpEnabled={workspace.totpEnabled}
    />
  )

  const workspacePage = workspace.auth.authenticated ? (
    <Layout className="app-shell">
      {!isMobile && (
        <Sider
          ref={sidebarElementRef}
          className={`sidebar ${sidebarCollapsed ? 'collapsed' : ''}`}
          width={sidebarCollapsed ? 72 : sidebarWidth}
          collapsedWidth={72}
          style={{ width: sidebarCollapsed ? 72 : sidebarWidth }}
        >
          {sidebar}
          {!sidebarCollapsed ? (
            <div
              className="sidebar-resizer"
              onPointerDown={(event) => {
                event.preventDefault()
                sidebarResizeRef.current = {
                  startX: event.clientX,
                  startWidth: sidebarWidth,
                }
                document.body.classList.add('is-resizing-sidebar')
              }}
            />
          ) : null}
        </Sider>
      )}
      <Layout className="main-layout">
        <Header className="header-shell">
          <div className="header-glow" />
        </Header>
        <Content className="content-shell">
          <ChatPane
            availableToolSchemas={workspace.availableToolSchemas}
            chatScrollRef={workspace.chatScrollRef}
            composer={workspace.composer}
            composerMode={workspace.composerMode}
            draftBuffer={workspace.draftBuffer}
            handleComposerKeyDown={workspace.handleComposerKeyDown}
            isMobile={isMobile}
            isWaitingForUser={workspace.isWaitingForUser}
            keyboardOffset={workspace.keyboardOffset}
            messagesLoading={workspace.messagesLoading}
            onDraft={workspace.handleDraft}
            onLogout={workspace.handleLogout}
            onOpenDrawer={() => workspace.setDrawerOpen(true)}
            onSend={workspace.handleSend}
            selectedConversationId={workspace.selectedConversationId}
            selectedConversationTitle={workspace.selectedConversation?.title || ''}
            selectedRequestFormat={workspace.selectedRequestFormat}
            selectedToolSchema={workspace.selectedToolSchema}
            sending={workspace.sending}
            setComposer={workspace.setComposer}
            setComposerMode={workspace.setComposerMode}
            setThinkingText={workspace.setThinkingText}
            setReasoningStreamMode={workspace.setReasoningStreamMode}
            setToolCallId={workspace.setToolCallId}
            setToolFormValues={workspace.setToolFormValues}
            setToolName={workspace.setToolName}
            thinkingText={workspace.thinkingText}
            reasoningStreamMode={workspace.reasoningStreamMode}
            toolCallId={workspace.toolCallId}
            toolFormValues={workspace.toolFormValues}
            toolName={workspace.toolName}
            visibleMessages={workspace.visibleMessages}
          />
        </Content>
      </Layout>
      <Drawer
        open={workspace.drawerOpen}
        onClose={() => workspace.setDrawerOpen(false)}
        placement="left"
        width={320}
        className="mobile-drawer"
        bodyStyle={{ padding: 0 }}
      >
        {sidebar}
      </Drawer>
    </Layout>
  ) : (
    <Navigate to="/login" replace />
  )

  return workspacePage
}
