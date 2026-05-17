import { useEffect, useRef, useState } from 'react'
import { App as AntApp, Drawer, Grid, Layout, Spin } from 'antd'

import './App.css'
import { ChatPane } from './components/ChatPane'
import { ConversationSidebar } from './components/ConversationSidebar'
import { LoginScreen } from './components/LoginScreen'
import { useChatWorkspace } from './hooks/useChatWorkspace'

const { Header, Sider, Content } = Layout
const SIDEBAR_COLLAPSED_KEY = 'chatapi.sidebar.collapsed'
const SIDEBAR_WIDTH_KEY = 'chatapi.sidebar.width'

function App() {
  const screens = Grid.useBreakpoint()
  const isMobile = !screens.md
  const workspace = useChatWorkspace(isMobile)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    if (typeof window === 'undefined') return false
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1'
  })
  const [sidebarWidth, setSidebarWidth] = useState(320)
  const sidebarResizeRef = useRef<{ startX: number; startWidth: number } | null>(null)

  useEffect(() => {
    if (typeof window === 'undefined') return
    const raw = window.localStorage.getItem(SIDEBAR_WIDTH_KEY)
    if (!raw) return
    const parsed = Number(raw)
    if (Number.isFinite(parsed)) {
      setSidebarWidth(Math.min(480, Math.max(220, parsed)))
    }
  }, [])

  useEffect(() => {
    if (isMobile) return

    function handlePointerMove(event: PointerEvent) {
      const current = sidebarResizeRef.current
      if (!current) return
      const nextWidth = Math.min(480, Math.max(220, current.startWidth + event.clientX - current.startX))
      setSidebarWidth(nextWidth)
    }

    function handlePointerUp() {
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

  if (workspace.booting) {
    return (
      <div className="boot-screen">
        <Spin size="large" />
      </div>
    )
  }

  if (!workspace.auth.authenticated) {
    return (
      <LoginScreen
        loading={workspace.loginLoading}
        onSubmit={workspace.handleLogin}
      />
    )
  }

  const sidebar = (
    <ConversationSidebar
      abortPopoverConversationId={workspace.abortPopoverConversationId}
      abortReason={workspace.abortReason}
      abortingConversationId={workspace.abortingConversationId}
      auth={workspace.auth}
      collapsed={!isMobile && sidebarCollapsed}
      conversations={workspace.conversations}
      deletingConversationId={workspace.deletingConversationId}
      onAbortConversation={workspace.handleAbortConversation}
      onDeleteConversation={workspace.handleDeleteConversation}
      onLogout={workspace.handleLogout}
      onPruneConversations={workspace.handlePruneConversations}
      onSelectConversation={workspace.handleSelectConversation}
      onToggleCollapsed={() => setSidebarCollapsed((value) => !value)}
      pruneKeepCount={workspace.pruneKeepCount}
      pruneModalOpen={workspace.pruneModalOpen}
      pruningConversations={workspace.pruningConversations}
      savingStreamHeartbeatConfig={workspace.savingStreamHeartbeatConfig}
      selectedConversationId={workspace.selectedConversationId}
      setAbortPopoverConversationId={workspace.setAbortPopoverConversationId}
      setAbortReason={workspace.setAbortReason}
      setPruneKeepCount={workspace.setPruneKeepCount}
      setPruneModalOpen={workspace.setPruneModalOpen}
      setStreamHeartbeatIntervalSeconds={workspace.setStreamHeartbeatIntervalSeconds}
      setStreamHeartbeatModalOpen={workspace.setStreamHeartbeatModalOpen}
      setStreamHeartbeatText={workspace.setStreamHeartbeatText}
      streamHeartbeatIntervalSeconds={workspace.streamHeartbeatIntervalSeconds}
      streamHeartbeatModalOpen={workspace.streamHeartbeatModalOpen}
      streamHeartbeatText={workspace.streamHeartbeatText}
      onSaveStreamHeartbeatConfig={workspace.handleSaveStreamHeartbeatConfig}
    />
  )

  return (
    <AntApp>
      <Layout className="app-shell">
        {!isMobile && (
          <div
            className={`sidebar-shell ${sidebarCollapsed ? 'collapsed' : ''}`}
            style={{ width: sidebarCollapsed ? 72 : sidebarWidth }}
          >
            <Sider className="sidebar" width={sidebarCollapsed ? 72 : sidebarWidth}>
              {sidebar}
            </Sider>
            {!sidebarCollapsed ? (
              <div
                className="sidebar-resizer"
                onPointerDown={(event) => {
                  sidebarResizeRef.current = {
                    startX: event.clientX,
                    startWidth: sidebarWidth,
                  }
                  document.body.classList.add('is-resizing-sidebar')
                }}
              />
            ) : null}
          </div>
        )}
        <Layout className="main-layout">
          <Header className="header-shell">
            <div className="header-glow" />
          </Header>
          <Content className="content-shell">
            <ChatPane
              availableToolSchemas={workspace.availableToolSchemas}
              bottomRef={workspace.bottomRef}
              chatScrollRef={workspace.chatScrollRef}
              composer={workspace.composer}
              composerMode={workspace.composerMode}
              draftBuffer={workspace.draftBuffer}
              handleChatScroll={workspace.handleChatScroll}
              handleComposerKeyDown={workspace.handleComposerKeyDown}
              isMobile={isMobile}
              isWaitingForUser={workspace.isWaitingForUser}
              keyboardOffset={workspace.keyboardOffset}
              onDraft={workspace.handleDraft}
              onLogout={workspace.handleLogout}
              onOpenDrawer={() => workspace.setDrawerOpen(true)}
              onSend={workspace.handleSend}
              selectedConversationTitle={workspace.selectedConversation?.title || ''}
              selectedToolSchema={workspace.selectedToolSchema}
              sending={workspace.sending}
              setComposer={workspace.setComposer}
              setComposerMode={workspace.setComposerMode}
              setToolCallId={workspace.setToolCallId}
              setToolFormValues={workspace.setToolFormValues}
              setToolName={workspace.setToolName}
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
    </AntApp>
  )
}

export default App
