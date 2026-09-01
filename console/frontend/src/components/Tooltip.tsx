import {
  Children, cloneElement, isValidElement,
  useCallback, useEffect, useId, useRef, useState,
} from 'react'
import { createPortal } from 'react-dom'

/**
 * A themed replacement for the native `title` attribute.
 *
 * `title` renders an OS tooltip: unstyleable, ~1s before it appears, and set in
 * the system UI font, which reads as a browser artifact sitting on top of the
 * console rather than part of it. The header fields carry values an operator
 * actually needs to read — a 90-character Azure resource group that ellipsizes
 * in the row — so the reveal is part of the interface, not a footnote.
 *
 * Rendered into a portal on `position: fixed` coordinates. The header row
 * ellipsizes its fields, so a tooltip positioned inside it would be clipped by
 * the same overflow that made the tooltip necessary; a portal escapes that and
 * every stacking context along the way.
 */

/** Gap between the trigger and the bubble, leaving room for the arrow. */
const OFFSET = 8
/** Keeps the bubble off the viewport edge when a field sits near one. */
const MARGIN = 8
/** Long enough that sweeping the pointer across a row stays quiet. */
const DELAY_MS = 120

export interface Position {
  left: number
  top: number
  /** Below the trigger when there isn't room above; the arrow follows. */
  below: boolean
  /** Arrow's x offset within the bubble — it stays on the trigger's centre
   *  even when the bubble is pushed sideways by the viewport clamp. */
  arrowLeft: number
}

/** Just the geometry each side of the placement needs. */
export interface Box {
  left: number
  top: number
  right: number
  bottom: number
  width: number
  height: number
}

/**
 * Where the bubble goes for a given trigger, bubble size and viewport.
 *
 * Pure and exported so the cases that actually break tooltips can be tested
 * without a browser: a trigger at the top of the window (must flip below), and
 * one against either edge (must clamp, with the arrow staying on the trigger
 * rather than sliding to the bubble's middle).
 *
 * Prefers above — the header sits at the top of the pane, and a bubble below it
 * would cover the topology it describes.
 */
export function placeTooltip(
  anchor: Box, bubble: { width: number; height: number }, viewport: { width: number },
): Position {
  const below = anchor.top - bubble.height - OFFSET < MARGIN
  const top = below ? anchor.bottom + OFFSET : anchor.top - bubble.height - OFFSET

  const centred = anchor.left + anchor.width / 2 - bubble.width / 2
  // Math.max last so the left edge wins on a viewport narrower than the bubble;
  // clamping right-first there would push it off the left instead.
  const left = Math.max(
    MARGIN,
    Math.min(centred, viewport.width - bubble.width - MARGIN),
  )
  return { left, top, below, arrowLeft: anchor.left + anchor.width / 2 - left }
}

/** Idle → what the click did. Mirrors the connect modal's copy button. */
type CopyState = 'idle' | 'copied' | 'failed'

export default function Tooltip(
  { label, copy, children }: {
    label: string
    /** When set, clicking the trigger copies this. The header values are the
     *  kind you paste into a terminal — a resource id, a subscription — and
     *  they ellipsize, so selecting them by hand is the one thing you cannot
     *  do. Omit for tooltips that are only prose. */
    copy?: string
    children: React.ReactNode
  },
) {
  const [pos, setPos] = useState<Position | null>(null)
  const [copied, setCopied] = useState<CopyState>('idle')
  const holder = useRef<HTMLSpanElement>(null)
  const bubble = useRef<HTMLDivElement>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const id = useId()

  const hide = useCallback(() => {
    clearTimeout(timer.current)
    setPos(null)
    // Reset on leave rather than on a timer: the confirmation belongs to this
    // hover, and a stale "copied" on the next one would claim something that
    // did not happen.
    setCopied('idle')
  }, [])

  const doCopy = useCallback(() => {
    if (!copy) return
    // Same contract as the connect modal: localhost is a secure context so the
    // async API exists, but it still rejects when the document is unfocused or
    // permission is denied — and a copy that silently does nothing is worse
    // than one that says so.
    navigator.clipboard?.writeText(copy)
      .then(() => setCopied('copied'))
      .catch(() => setCopied('failed'))
  }, [copy])

  // Measure on the frame after the bubble mounts: its size depends on the text,
  // and placement needs the real box, not an estimate.
  const place = useCallback(() => {
    // The rendered child, not the wrapper. A display:contents element generates
    // no box, so getBoundingClientRect() on it returns all zeros — which placed
    // every tooltip in the top-left corner of the viewport instead of over its
    // field. Caught by driving the real component in a browser; the numbers
    // came back as exactly the viewport margin, which is what a zero anchor
    // clamps to.
    const anchor = holder.current?.firstElementChild?.getBoundingClientRect()
    if (!anchor) return
    const box = bubble.current?.getBoundingClientRect()
    setPos(placeTooltip(
      anchor,
      { width: box?.width ?? 0, height: box?.height ?? 0 },
      { width: window.innerWidth },
    ))
  }, [])

  const show = useCallback(() => {
    clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      const anchor = holder.current?.firstElementChild
      if (!anchor) return
      // Render off-measure first, then place: `below` is a guess until the
      // bubble exists, and a visible reposition would read as a jump.
      setPos({ left: -9999, top: -9999, below: false, arrowLeft: 0 })
    }, DELAY_MS)
  }, [])

  useEffect(() => {
    if (pos && pos.left === -9999) place()
  }, [pos, place])

  // A tooltip pinned to a stale position is worse than none: the trigger moves
  // when the header wraps or the pane is resized, so close instead of chasing.
  useEffect(() => {
    if (!pos) return
    window.addEventListener('scroll', hide, true)
    window.addEventListener('resize', hide)
    return () => {
      window.removeEventListener('scroll', hide, true)
      window.removeEventListener('resize', hide)
    }
  }, [pos, hide])

  useEffect(() => () => clearTimeout(timer.current), [])

  const measuring = pos !== null && pos.left === -9999

  // Focus, tab order and the focus ring all need a box, and the wrapper has
  // none — a display:contents element cannot be focused at all, so tabIndex on
  // it is silently inert (verified: activeElement stayed on <body>). Put those
  // on the child, which is the thing actually drawn. The pointer and focus
  // handlers can stay on the wrapper: React's onFocus/onBlur use focusin and
  // focusout, and enter/leave are synthesised from delegated pointerover, so
  // all of them still reach it from the child.
  const only = Children.only(children)
  const trigger = copy && isValidElement<{ className?: string }>(only)
    ? cloneElement(only, {
      tabIndex: 0,
      role: 'button',
      'aria-label': `Copy ${label}`,
      className: [only.props.className, 'dg-tip-copy'].filter(Boolean).join(' '),
    } as Partial<{ className?: string }>)
    : children

  return (
    <>
      <span
        ref={holder}
        // contents: the wrapper must not become a box of its own — the header
        // lays its fields out directly and an extra inline box would change
        // both the flex sizing and where the ellipsis falls. It generates no
        // box, so cursor (inherited) reaches the child but a focus ring cannot
        // paint here — the class goes on the child instead (see `trigger`).
        style={{ display: 'contents' }}
        onPointerEnter={show}
        onPointerLeave={hide}
        // Keyboard parity: the value is only reachable on hover otherwise.
        onFocus={show}
        onBlur={hide}
        onClick={copy ? doCopy : undefined}
        // Enter/Space is what a button does, and this behaves as one when it
        // copies. Not focusable without `copy` — a tooltip that only shows
        // prose is not a control and should not be a tab stop.
        onKeyDown={copy ? (e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            doCopy()
          }
        } : undefined}
        aria-describedby={pos ? id : undefined}
      >
        {trigger}
      </span>
      {pos && createPortal(
        <div
          ref={bubble}
          id={id}
          role="tooltip"
          style={{
            position: 'fixed', left: pos.left, top: pos.top, zIndex: 1000,
            // Hidden while measuring rather than unmounted, so the box it
            // reports is the box that will be shown.
            visibility: measuring ? 'hidden' : 'visible',
            pointerEvents: 'none',
            maxWidth: 'min(420px, calc(100vw - 16px))',
            padding: '6px 9px',
            background: 'var(--dn-surface)',
            border: '1px solid var(--dn-border-lt)',
            borderRadius: 4,
            // Lifts it off whatever it covers; the panels are near-black, so a
            // border alone leaves the bubble looking like a hole in the page.
            boxShadow: '0 4px 14px rgba(0, 0, 0, 0.55)',
            color: 'var(--dn-text-bright)',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            lineHeight: 1.45,
            // Long values (resource ids, CIDRs) are the reason this exists:
            // wrap them rather than running off the edge, and break mid-token
            // since they have no spaces to break at.
            whiteSpace: 'pre-wrap',
            overflowWrap: 'anywhere',
            fontVariantNumeric: 'tabular-nums',
          }}
        >
          {label}
          {copy && (
            // The affordance has to be stated: nothing about a header field
            // looks clickable, so an unannounced copy is a feature nobody
            // finds. Same line height as the label so the bubble does not
            // resize between states and jitter under the pointer.
            <div style={{
              marginTop: 4, fontSize: 10,
              color: copied === 'failed'
                ? 'var(--dn-error)'
                : copied === 'copied' ? 'var(--dn-success)' : 'var(--dg-node-label)',
            }}>
              {copied === 'copied' ? '✓ copied'
                : copied === 'failed' ? 'clipboard blocked'
                  : 'click to copy'}
            </div>
          )}
          {/* Two stacked squares: the back one carries the border colour, the
              front one the surface, so the arrow reads as an outlined notch
              rather than a floating diamond. */}
          <span style={{
            position: 'absolute',
            left: Math.max(8, Math.min(pos.arrowLeft, 400)) - 4,
            [pos.below ? 'top' : 'bottom']: -4,
            width: 7, height: 7,
            background: 'var(--dn-surface)',
            borderRight: '1px solid var(--dn-border-lt)',
            borderBottom: '1px solid var(--dn-border-lt)',
            transform: pos.below ? 'rotate(-135deg)' : 'rotate(45deg)',
          }} />
        </div>,
        document.body,
      )}
    </>
  )
}
