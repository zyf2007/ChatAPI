import { App as AntApp, Drawer, Grid, Layout, Spin } from 'antd'

import './App.css'
import { ChatPane } from './components/ChatPane'
import { ConversationSidebar } from './components/ConversationSidebar'
import { LoginScreen } from './components/LoginScreen'
import { useChatWorkspace } from './hooks/useChatWorkspace'

const { Header, Sider, Content } = Layout

function App() {
  const screens = Grid.useBreakpoint()
  const isMobile = !screens.md
  const workspace = useChatWorkspace(isMobile)

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
      conversations={workspace.conversations}
      deletingConversationId={workspace.deletingConversationId}
      onAbortConversation={workspace.handleAbortConversation}
      onDeleteConversation={workspace.handleDeleteConversation}
      onLogout={workspace.handleLogout}
      onPruneConversations={workspace.handlePruneConversations}
      onSelectConversation={workspace.handleSelectConversation}
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
        {!isMobile && <Sider className="sidebar">{sidebar}</Sider>}
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
              setDraftBuffer={workspace.setDraftBuffer}
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
