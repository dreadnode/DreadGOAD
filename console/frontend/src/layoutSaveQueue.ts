import type { RangeLayout } from './types'

type SaveLayout = (layout: RangeLayout, revision: number) => Promise<number>

/**
 * Serializes layout writes and collapses rapid drags to the newest snapshot.
 *
 * Only one request may be in flight. If several drag-stop events arrive while
 * it is running, intermediate layouts are replaced by the latest one, which is
 * sent using the revision returned by the preceding save.
 */
export class LatestLayoutSaver {
  private pending: RangeLayout | null = null
  private drainPromise: Promise<void> | null = null
  private revision = 0

  constructor(
    private readonly save: SaveLayout,
    private readonly onError: (error: unknown) => void,
  ) {}

  setRevision(revision: number): void {
    if (!this.drainPromise && !this.pending && Number.isInteger(revision) && revision >= 0) {
      this.revision = revision
    }
  }

  enqueue(layout: RangeLayout): void {
    this.pending = Object.fromEntries(
      Object.entries(layout).map(([id, position]) => [id, { ...position }]),
    )
    if (!this.drainPromise) {
      this.drainPromise = Promise.resolve()
        .then(() => this.drain())
        .finally(() => { this.drainPromise = null })
    }
  }

  /** Resolves when all currently queued work has settled; useful for tests. */
  whenIdle(): Promise<void> {
    return this.drainPromise ?? Promise.resolve()
  }

  private async drain(): Promise<void> {
    while (this.pending) {
      const layout = this.pending
      this.pending = null
      try {
        this.revision = await this.save(layout, this.revision)
      } catch (error) {
        this.pending = null
        this.onError(error)
        return
      }
    }
  }
}
