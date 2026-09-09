import { useState } from 'react'
import { api, type AppConfig } from '../api'
import { Field, btnStyle } from './FormFields'
import Modal from './Modal'

export default function SettingsModal({ cfg, model, onModelChange, onClose, onSaved }: {
  cfg: AppConfig
  model?: string
  onModelChange?: (model: string) => Promise<void> | void
  onClose: () => void
  onSaved: () => void
}) {
  const [modelInput, setModelInput] = useState(model ?? '')
  const [apiKey, setApiKey] = useState('')
  const [apiKeyEnv, setApiKeyEnv] = useState('OPENROUTER_API_KEY')
  const [err, setErr] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    setErr('')
    setSaving(true)
    try {
      const nextModel = modelInput.trim()
      if (onModelChange && nextModel && nextModel !== model) await onModelChange(nextModel)
      if (apiKey.trim()) {
        await api.setSettings({ api_key: apiKey.trim(), api_key_env: apiKeyEnv.trim() || undefined })
      }
      onSaved()
    } catch (error) {
      setErr(error instanceof Error ? error.message : String(error))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal onClose={onClose} width={420} ariaLabel="Settings">
      <div style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 16 }}>Settings</div>

      {onModelChange ? (
        <>
          <Field label="Model (this session)" value={modelInput} onChange={setModelInput} placeholder="openrouter/anthropic/claude-sonnet-5" />
          <div style={{ color: 'var(--dn-text-dim)', fontSize: 10, marginTop: -8, marginBottom: 12 }}>
            Changing the model continues this session's conversation on the new model.
          </div>
        </>
      ) : (
        <div style={{ color: 'var(--dn-text-dim)', fontSize: 11, marginBottom: 12 }}>
          Open a session to change its model.
        </div>
      )}

      <Field label="API key" value={apiKey} onChange={setApiKey} placeholder="sk-or-…  (stored in memory, never saved)" type="password" />
      <Field label="API key env var" value={apiKeyEnv} onChange={setApiKeyEnv} placeholder="OPENROUTER_API_KEY" />
      <div style={{ color: cfg.api_key_set ? 'var(--dn-success)' : 'var(--dn-warning)', fontSize: 11, marginTop: -4, marginBottom: 12 }}>
        {cfg.api_key_set ? '● API key is set (leave blank to keep)' : '○ No API key set — agent turns will fail'}
      </div>

      {err && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginBottom: 8 }}>{err}</div>}
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
        <button onClick={onClose} style={btnStyle(false)}>CANCEL</button>
        <button onClick={save} disabled={saving} style={btnStyle(true)}>{saving ? 'SAVING…' : 'SAVE'}</button>
      </div>
    </Modal>
  )
}
