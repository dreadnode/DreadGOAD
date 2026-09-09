/** Flavour text for the "Agent …" indicator while a turn is in flight.
 *
 * Present participles, so each drops into the same slot "working" occupied.
 *
 * Deliberately excludes anything the platform actually does — destroying,
 * terminating, purging, wiping. This console really can tear down a range, and
 * a status line reading "Agent destroying" while the agent is quietly reading a
 * config would be a genuinely alarming thing to walk in on.
 */
export const AGENT_VERBS = [
  'exhuming',
  'festering',
  'haunting',
  'skulking',
  'entombing',
  'desecrating',
  'embalming',
  'withering',
  'putrefying',
  'moldering',
  'defiling',
  'lurking',
  'brooding',
  'gnawing',
  'writhing',
  'seething',
  'smoldering',
  'decaying',
  'unearthing',
  'disinterring',
  'shrouding',
  'interring',
  'mourning',
  'keening',
  'conjuring',
  'summoning',
  'invoking',
  'cursing',
  'hexing',
  'blighting',
  'plaguing',
  'infesting',
  'devouring',
  'flensing',
  'marauding',
  'prowling',
  'stalking',
  'creeping',
  'slithering',
  'ossifying',
] as const

/**
 * Pick a verb from a seed.
 *
 * Pure, so the caller controls exactly when a new word is drawn. That matters:
 * the indicator re-renders every second because the stopwatch beside it ticks,
 * so choosing during render would reshuffle the word once a second instead of
 * once a turn. TerminalChat calls this once on the turn's leading edge and
 * holds the result for the turn's duration.
 *
 * The seeds of consecutive turns are near-adjacent integers, which a plain
 * modulo would map to adjacent list entries and march through the list in
 * order. Hashing first scatters them.
 */
export function agentVerb(seed: number | null | undefined): string {
  let h = (seed ?? 0) | 0
  h = Math.imul(h ^ (h >>> 15), 0x2c1b3c6d)
  h = Math.imul(h ^ (h >>> 12), 0x297a2d39)
  h ^= h >>> 15
  // >>> 0 rather than Math.abs: the hash can land on -2^31, whose absolute
  // value is not representable as a positive int32 and stays negative.
  return AGENT_VERBS[(h >>> 0) % AGENT_VERBS.length]
}
