import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server.browser'
import ConfirmModal, { commandTokens, confirmActionForKey, formatArgvForShell } from '../src/components/ConfirmModal'

function equal(actual: unknown, expected: unknown, label: string) {
  if (actual !== expected) throw new Error(`${label}: got ${String(actual)}`)
}

equal(confirmActionForKey('c'), 'confirm', 'lowercase confirm')
equal(confirmActionForKey('C'), 'confirm', 'uppercase confirm')
equal(confirmActionForKey('d'), 'deny', 'lowercase deny')
equal(confirmActionForKey('D'), 'deny', 'uppercase deny')
equal(confirmActionForKey('Enter'), null, 'unrelated key')
equal(
  formatArgvForShell(['dreadgoad', '--cmd', "Write-Host 'hello world'"]),
  "dreadgoad --cmd 'Write-Host '\\''hello world'\\'''",
  'shell-safe exact command',
)
equal(
  commandTokens(['dreadgoad', 'up', '--provider', 'aws']).map(token => token.kind).join(','),
  'executable,subcommand,flag,value',
  'command syntax token classes',
)

const modalMarkup = renderToStaticMarkup(createElement(ConfirmModal, {
  title: 'Run up?',
  message: 'Session: test',
  codeLabel: 'Exact command',
  commandArgv: ['dreadgoad', 'up', '--provider', 'aws'],
  onConfirm: () => {},
  onCancel: () => {},
}))
equal(modalMarkup.includes('Exact command'), true, 'exact command label')
equal(modalMarkup.includes('<pre'), true, 'command preformatted block')
equal(modalMarkup.includes('<code>'), true, 'command code element')
equal(modalMarkup.includes('data-command-token="executable"'), true, 'executable syntax color')
equal(modalMarkup.includes('data-command-token="subcommand"'), true, 'subcommand syntax color')
equal(modalMarkup.includes('data-command-token="flag"'), true, 'flag syntax color')
equal(modalMarkup.includes('data-command-token="value"'), true, 'value syntax color')
equal(modalMarkup.includes('>CONFIRM</button>'), true, 'confirm button label')

console.log('PASS confirm modal keyboard decisions and command rendering')
