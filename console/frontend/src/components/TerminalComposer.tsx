import { useEffect, useMemo, useRef, useState } from 'react'
import type { CommandDef } from '../api'
import type { ConnectionStatus } from '../hooks/useWebSocket'
import type { ChatEvent } from '../types'
import ConfirmModal from './ConfirmModal'
import { COPY_COMMAND, HELP_COMMAND } from './terminalCommands'
import { mergeHistory, type ClientOnlyEntry } from './terminalChatHistory'

interface Props {
  sessionId: string | null
  messages: ChatEvent[]
  status: ConnectionStatus
  processing: boolean
  commands: CommandDef[]
  catalogOk: boolean
  confirmationBlocked?: boolean
  onSend: (content: string) => void
  onCancel: () => void
  onShowHelp: (after: number) => void
}

interface PendingConfirmation {
  title: string
  message: string
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => void
}

export default function TerminalComposer({
  sessionId,
  messages,
  status,
  processing,
  commands,
  catalogOk,
  confirmationBlocked,
  onSend,
  onCancel,
  onShowHelp,
}: Props) {
  const [input, setInput] = useState('')
  const [cmdHighlight, setCmdHighlight] = useState(0)
  const [histIndex, setHistIndex] = useState<number | null>(null)
  const [draft, setDraft] = useState('')
  const [clientOnly, setClientOnly] = useState<ClientOnlyEntry[]>([])
  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirmation | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const activeCmdRef = useRef<HTMLDivElement>(null)

  const history = useMemo(
    () => mergeHistory(messages, clientOnly),
    [messages, clientOnly],
  )
  const firstToken = input.split(' ')[0]
  const filteredCommands = useMemo(
    () => (input.startsWith('/') ? commands.filter(c => c.name.startsWith(firstToken)) : []),
    [commands, input, firstToken],
  )
  const showCmdMenu = filteredCommands.length > 0
    && !input.includes(' ')
    && histIndex === null

  useEffect(() => {
    const element = inputRef.current
    if (!element) return
    element.style.height = 'auto'
    element.style.height = `${Math.min(element.scrollHeight, 200)}px`
  }, [input])

  useEffect(() => {
    activeCmdRef.current?.scrollIntoView({ block: 'nearest' })
  }, [cmdHighlight])

  useEffect(() => {
    if (histIndex === null) return
    const element = inputRef.current
    if (element) element.setSelectionRange(element.value.length, element.value.length)
  }, [histIndex, input])

  useEffect(() => {
    setHistIndex(null)
    setDraft('')
    setClientOnly([])
    setPendingConfirm(null)
  }, [sessionId])

  // Esc cancels an active turn unless it is currently closing autocomplete.
  useEffect(() => {
    if (!processing || showCmdMenu) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [processing, showCmdMenu, onCancel])

  const rememberClientOnly = (text: string) => {
    setClientOnly(previous => (
      previous[previous.length - 1]?.text === text
        && previous[previous.length - 1]?.after === messages.length
        ? previous
        : [...previous, { text, after: messages.length }]
    ))
  }

  const clearAfterSubmit = () => {
    setInput('')
    setCmdHighlight(0)
    setHistIndex(null)
  }

  const submit = () => {
    const text = input.trim()
    if (!text) return

    if (text === HELP_COMMAND.name) {
      onShowHelp(messages.length)
      rememberClientOnly(text)
      clearAfterSubmit()
      return
    }

    if (text.startsWith(COPY_COMMAND.name)) {
      const countArg = text.slice(COPY_COMMAND.name.length).trim()
      const all = messages.flatMap(message => (
        message.kind === 'generation' && message.content ? [message.content] : []
      ))
      const copyAll = countArg.toLowerCase() === 'all'
      const parsed = countArg && !copyAll ? parseInt(countArg, 10) : 1
      const count = isNaN(parsed) || parsed <= 0 ? 1 : parsed
      const selected = copyAll ? all : all.slice(-count)
      if (selected.length > 0) {
        navigator.clipboard.writeText(selected.join('\n\n')).catch(() => {})
      }
      rememberClientOnly(text)
      clearAfterSubmit()
      return
    }

    if (!sessionId || status !== 'connected' || processing) return

    const send = () => {
      onSend(text)
      clearAfterSubmit()
      setDraft('')
    }
    if (!catalogOk && text.startsWith('/')) {
      setPendingConfirm({
        title: 'Unverified command',
        message: `The command list could not be loaded, so ${text.split(' ')[0]} cannot be `
          + 'checked for whether it is destructive.\n\nRun it anyway?',
        destructive: true,
        onConfirm: () => {
          send()
          setPendingConfirm(null)
        },
      })
      return
    }
    send()
  }

  const selectCommand = (command: CommandDef) => {
    setInput(`${command.name} `)
    setCmdHighlight(0)
    inputRef.current?.focus()
  }

  const recallOlder = (): boolean => {
    if (history.length === 0) return false
    if (histIndex === null) setDraft(input)
    const next = histIndex === null ? 0 : histIndex + 1
    if (next >= history.length) return true
    setHistIndex(next)
    setInput(history[history.length - 1 - next])
    return true
  }

  const recallNewer = (): boolean => {
    if (histIndex === null) return false
    if (histIndex === 0) {
      setHistIndex(null)
      setInput(draft)
      return true
    }
    const next = histIndex - 1
    setHistIndex(next)
    setInput(history[history.length - 1 - next])
    return true
  }

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (showCmdMenu) {
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setCmdHighlight(index => (index > 0 ? index - 1 : filteredCommands.length - 1))
        return
      }
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setCmdHighlight(index => (index < filteredCommands.length - 1 ? index + 1 : 0))
        return
      }
      if (event.key === 'Tab' || (event.key === 'Enter' && !event.shiftKey)) {
        event.preventDefault()
        selectCommand(filteredCommands[cmdHighlight])
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        setInput('')
        return
      }
    }

    const element = event.currentTarget
    const noSelection = element.selectionStart === element.selectionEnd
    const onFirstLine = noSelection
      && !element.value.slice(0, element.selectionStart).includes('\n')
    const onLastLine = noSelection
      && !element.value.slice(element.selectionEnd).includes('\n')
    if (event.key === 'ArrowUp' && !event.shiftKey && onFirstLine && recallOlder()) {
      event.preventDefault()
      return
    }
    if (event.key === 'ArrowDown' && !event.shiftKey && onLastLine && recallNewer()) {
      event.preventDefault()
      return
    }
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      submit()
    }
  }

  return (
    <>
      {!confirmationBlocked && pendingConfirm && (
        <ConfirmModal
          title={pendingConfirm.title}
          message={pendingConfirm.message}
          confirmLabel={pendingConfirm.confirmLabel}
          destructive={pendingConfirm.destructive}
          onConfirm={pendingConfirm.onConfirm}
          onCancel={() => setPendingConfirm(null)}
        />
      )}
      <div style={{ position: 'relative', borderTop: '1px solid var(--dn-border)', background: 'var(--dn-black)' }}>
        {showCmdMenu && (
          <div style={{
            position: 'absolute', bottom: '100%', left: 0, right: 0, zIndex: 50,
            background: 'var(--dn-surface)', border: '1px solid var(--dn-border)',
            borderBottom: 'none', borderRadius: '4px 4px 0 0',
            maxHeight: 300, overflowY: 'auto', fontFamily: 'var(--font-mono)', fontSize: 12,
          }}>
            {filteredCommands.map((command, index) => (
              <div
                key={command.name}
                ref={index === cmdHighlight ? activeCmdRef : undefined}
                onMouseDown={event => {
                  event.preventDefault()
                  selectCommand(command)
                }}
                onMouseEnter={() => setCmdHighlight(index)}
                style={{
                  padding: '7px 12px', cursor: 'pointer', display: 'flex', gap: 8,
                  alignItems: 'flex-start',
                  background: index === cmdHighlight ? 'var(--dn-border)' : 'transparent',
                }}
              >
                <span title={command.dispatch === 'agent'
                  ? 'agent — interprets free-form arguments into CLI flags'
                  : 'direct — runs the CLI verb as-is, no LLM involved'}
                >{command.dispatch === 'agent' ? '🤖' : '⚡'}</span>
                <span style={{ color: 'var(--dg-interactive)', minWidth: 96, flexShrink: 0 }}>
                  {command.name}
                </span>
                <span style={{ minWidth: 0 }}>
                  <span style={{ color: 'var(--dn-text-bright)', fontSize: 11 }}>
                    {command.description}
                  </span>
                  <span style={{ display: 'block', fontSize: 10, marginTop: 2 }}>
                    {command.detail && (
                      <span style={{ color: 'var(--dg-node-label)' }}>{command.detail}</span>
                    )}
                    {command.cli && (
                      <span style={{ color: 'var(--dn-text-muted)' }}>
                        {command.detail ? '  ·  ' : ''}{command.cli}
                      </span>
                    )}
                  </span>
                </span>
              </div>
            ))}
          </div>
        )}
        <div style={{ display: 'flex', alignItems: 'flex-start', padding: '12px 16px' }}>
          <span style={{ color: 'var(--dg-interactive)', marginRight: 8, fontSize: 13, lineHeight: '20px' }}>&gt;</span>
          <textarea
            ref={inputRef}
            value={input}
            rows={1}
            disabled={!sessionId || status !== 'connected' || processing}
            onChange={event => {
              setInput(event.target.value)
              setCmdHighlight(0)
              setHistIndex(null)
            }}
            onKeyDown={handleKeyDown}
            placeholder={
              status !== 'connected'
                ? `${status}…`
                : !sessionId
                  ? 'create or select a session (+ NEW) to begin'
                  : processing
                    ? 'wait for the current turn, or press Esc to cancel'
                    : 'message or /command  (type / for commands)'
            }
            style={{
              flex: 1, background: 'transparent', border: 'none', outline: 'none', resize: 'none',
              color: 'var(--dn-text-bright)', fontFamily: 'var(--font-mono)', fontSize: 13,
              lineHeight: '20px', padding: 0, margin: 0,
              maxHeight: 200, overflowY: 'auto', display: 'block',
            }}
          />
        </div>
      </div>
    </>
  )
}
