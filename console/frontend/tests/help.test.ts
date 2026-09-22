import assert from 'node:assert/strict'
import type { CommandDef } from '../src/api'
import { buildHelpLines } from '../src/help'

function command(name: string): CommandDef {
  return {
    name,
    description: `${name} description`,
    detail: '',
    cli: name.slice(1),
    dispatch: 'direct',
    long_running: false,
    takes_args: false,
  }
}

const text = buildHelpLines([
  command('/up'),
  command('/scrub'),
  command('/stop'),
]).map(line => line.text).join('\n')

assert.match(text, /1\. Deploy the range/)
assert.match(text, /Builds and provisions end to end/)
assert.doesNotMatch(text, /\/variant description|fresh names and passwords/)
assert.doesNotMatch(text, /Score an agent run|Grades an agent report/)
assert.match(text, /2\. Reset for the next run/)
assert.match(text, /Deletes engagement artifacts/)
assert.doesNotMatch(text, /\/reset description|baseline restore/)
assert.match(text, /3\. Park it or tear it down/)
assert.doesNotMatch(text, /Brings the same stopped range back|Irreversibly deletes/)

console.log('PASS help hides unsupported phases and command guidance')
