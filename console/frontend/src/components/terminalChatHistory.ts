import type { ChatEvent } from '../types'

/** A command typed but never sent, tagged with where in the transcript it fell. */
export interface ClientOnlyEntry { text: string; after: number }

/** Build recall history from persisted messages and local-only commands. */
export function mergeHistory(
  messages: ChatEvent[], clientOnly: ClientOnlyEntry[],
): string[] {
  const out: string[] = []
  for (let i = 0; i <= messages.length; i++) {
    for (const entry of clientOnly) {
      if (entry.after === i) out.push(entry.text)
    }
    const message = messages[i]
    if (message?.kind === 'user_message') {
      const text = message.content.trim()
      if (text) out.push(text)
    }
  }
  for (const entry of clientOnly) {
    if (entry.after > messages.length) out.push(entry.text)
  }
  return out
}
