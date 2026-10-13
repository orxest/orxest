/**
 * Edge autoscroll for the kanban board.
 *
 * The board is wider than the viewport and its horizontal scrollbar is hidden,
 * so without this a card could only be dropped on a column that already happened
 * to be visible. The pointer is tracked on the document rather than on the board,
 * because dragging into an edge pushes the pointer outside the board (and onto
 * the scroll rail above it), where the board's own dragover events no longer
 * arrive.
 */

/** How far the pointer must reach from an edge before the board starts moving. */
export function autoScrollZone(width: number): number {
  // Narrow boards get a proportionally smaller zone so the middle of the board
  // never scrolls while a card is being placed on a visible column.
  return Math.min(140, Math.max(0, width) * 0.25)
}

/**
 * Scroll speed for a pointer position, in pixels per animation frame. Negative
 * scrolls left, positive right, and zero in the middle of the board.
 */
export function autoScrollSpeed(
  clientX: number,
  rect: { left: number; right: number; width: number },
  maxSpeed = 26,
): number {
  const zone = autoScrollZone(rect.width)
  if (zone <= 0) return 0
  if (clientX < rect.left + zone) {
    return -maxSpeed * Math.min(1, (rect.left + zone - clientX) / zone)
  }
  if (clientX > rect.right - zone) {
    return maxSpeed * Math.min(1, (clientX - (rect.right - zone)) / zone)
  }
  return 0
}

/** The scrollable surface the autoscroll drives. */
export interface AutoScrollTarget {
  scrollLeft: number
}

/**
 * Starts the autoscroll loop and returns a stop function. The speed is read on
 * every frame, so it keeps moving while the pointer is held still against an
 * edge, and the loop stops pushing once the board can no longer move.
 */
export function startAutoScroll(
  getTarget: () => AutoScrollTarget | null,
  getSpeed: () => number,
): () => void {
  let frame = 0
  const step = () => {
    const target = getTarget()
    const speed = getSpeed()
    if (target && speed !== 0) {
      const before = target.scrollLeft
      target.scrollLeft += speed
      // Clamp reported, so a held pointer does not busy-loop at the end.
      if (target.scrollLeft === before) return
    }
    frame = window.requestAnimationFrame(step)
  }
  frame = window.requestAnimationFrame(step)
  return () => window.cancelAnimationFrame(frame)
}
