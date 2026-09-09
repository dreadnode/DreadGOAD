import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server.browser'
import { Field, Select } from '../src/components/FormFields'

function assertLabelTargetsControl(markup: string, control: 'input' | 'select') {
  const labelTarget = markup.match(/<label for="([^"]+)"/)?.[1]
  if (!labelTarget) throw new Error(`${control} label has no target`)
  if (!markup.includes(`<${control} id="${labelTarget}"`)) {
    throw new Error(`${control} label target does not match the control id`)
  }
}

assertLabelTargetsControl(renderToStaticMarkup(createElement(Field, {
  label: 'Name',
  value: '',
  onChange: () => {},
  suggestions: ['range-one'],
})), 'input')

assertLabelTargetsControl(renderToStaticMarkup(createElement(Select, {
  label: 'Provider',
  value: 'aws',
  onChange: () => {},
  options: [{ value: 'aws', label: 'AWS' }],
})), 'select')

console.log('PASS form labels target their controls')
