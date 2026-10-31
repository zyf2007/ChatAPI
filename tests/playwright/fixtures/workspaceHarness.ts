import type { Page, WebSocketRoute } from '../../../frontend/node_modules/@playwright/test'
import type { Conversation, WorkspaceCommand } from '../../../frontend/src/types/chat'

/** Deterministic transport for frontend races; real Lab journeys use labWorkspace.ts. */
export async function installWorkspaceHarness(page: Page) {
  const conversations: Conversation[] = ['first', 'second'].map((id) => ({
    id, title: `Conversation ${id}`, last_user_text: id,
    created_at: '2026-10-31T00:00:00Z', updated_at: '2026-10-31T00:00:00Z',
    last_message_at: '2026-10-31T00:00:00Z', message_count: 1, last_message_preview: id,
    request_format: 'responses', request_id: `request_${id}`, status: 'waiting',
  }))
  await page.route('**/api/auth/session', (route) => route.fulfill({ json: {
    mode: 'lab', authenticated: true, user: { id: 'lab_default', username: 'lab', role: 'admin' },
    totp_enabled: false, registration_enabled: false, geetest_enabled: false,
    geetest_captcha_id: '', current_connection_count: 1, realtime_max_connections_per_user: 0,
    current_conversation_count: 2, user_conversation_limit: 0,
  } }))
  await page.route('**/api/automation/rules', (route) => route.fulfill({ json: { rules: [] } }))
  let socket: WebSocketRoute
  let resolveCommand: (command: WorkspaceCommand) => void
  const commandReceived = new Promise<WorkspaceCommand>((resolve) => { resolveCommand = resolve })
  await page.routeWebSocket('**/api/ws', (connection) => {
    socket = connection
    connection.onMessage((data) => {
      const payload = JSON.parse(String(data))
      if (payload.type === 'workspace.command') resolveCommand(payload.command)
      if (payload.type === 'timeline.subscribe') connection.send(JSON.stringify({
        type: 'timeline.reset', conversation_id: payload.conversation_id, items: [],
      }))
    })
    connection.send(JSON.stringify({
      type: 'workspace.snapshot', conversations, conversation_count: 2, has_more: false,
    }))
  })
  return {
    conversations,
    commandReceived,
    rejectCommand(command: WorkspaceCommand) {
      socket.send(JSON.stringify({
        type: 'workspace.command_error', command_id: command.command_id,
        conversation_id: command.conversation_id, request_id: command.request_id,
        code: 'stale_request', message: 'Request changed while sending',
      }))
    },
  }
}
