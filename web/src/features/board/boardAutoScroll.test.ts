import assert from 'node:assert/strict'
import { test } from 'node:test'

import { autoScrollSpeed, autoScrollZone, startAutoScroll } from './boardAutoScroll.ts'

// A board that spans 300..1300 of the viewport, the shape the columns row has
// when it is wider than the window.
const board = { left: 300, right: 1300, width: 1000 }

test('the middle of the board never scrolls', () => {
  for (const x of [440, 500, 800, 1000, 1150]) {
    assert.equal(autoScrollSpeed(x, board), 0, `expected no scroll at x=${x}`)
  }
})

test('the edges scroll in the matching direction', () => {
  assert.ok(autoScrollSpeed(300, board) < 0, 'left edge must scroll left')
  assert.ok(autoScrollSpeed(1300, board) > 0, 'right edge must scroll right')
  // Past the edge the pointer is clamped rather than accelerating forever.
  assert.equal(autoScrollSpeed(200, board), autoScrollSpeed(300, board))
  assert.equal(autoScrollSpeed(1400, board), autoScrollSpeed(1300, board))
})

test('speed ramps up towards the edge', () => {
  // The zone is the 140px inside each edge: 300..440 on the left. Speeds are
  // signed (negative scrolls left), so the ramp is compared by magnitude.
  const nearerMiddle = autoScrollSpeed(430, board)
  const nearerEdge = autoScrollSpeed(320, board)
  assert.ok(nearerMiddle < 0 && nearerEdge < 0, 'both points must be inside the zone')
  assert.ok(
    Math.abs(nearerEdge) > Math.abs(nearerMiddle),
    'the closer to the edge, the faster the scroll',
  )
  assert.equal(autoScrollSpeed(440, board), 0, 'the zone boundary is not a scroll zone')
})

test('a narrow board gets a proportionally smaller zone', () => {
  assert.equal(autoScrollZone(1000), 140)
  assert.equal(autoScrollZone(400), 100)
  assert.equal(autoScrollZone(0), 0)
  // A zero-width board must not divide by zero into a NaN speed.
  assert.equal(autoScrollSpeed(10, { left: 0, right: 0, width: 0 }), 0)
})

test('the loop applies the speed and stops at the end of the board', () => {
  const realWindow = globalThis.window
  const frames: Array<() => void> = []
  ;(globalThis as { window?: unknown }).window = {
    requestAnimationFrame: (callback: () => void) => {
      frames.push(callback)
      return frames.length
    },
    cancelAnimationFrame: () => {},
  }
  try {
    const target = { scrollLeft: 0, max: 200 }
    let speed = 10
    const stop = startAutoScroll(
      () => target,
      () => speed,
    )
    // Drive four frames by hand; each one should advance the board.
    for (let i = 0; i < 4; i += 1) frames.shift()?.()
    assert.ok(target.scrollLeft > 0, 'the board must scroll while a speed is set')

    // Reaching the end stops the loop rather than spinning on the clamp.
    target.scrollLeft = target.max
    const before = frames.length
    frames.shift()?.()
    assert.equal(frames.length, before, 'the loop must stop once the board cannot move')

    stop()
    // A zero speed also stops it.
    const target2 = { scrollLeft: 0 }
    const frames2: Array<() => void> = []
    ;(globalThis as { window: { requestAnimationFrame: (cb: () => void) => number } }).window.requestAnimationFrame =
      (callback: () => void) => {
        frames2.push(callback)
        return frames2.length
      }
    const stop2 = startAutoScroll(
      () => target2,
      () => 0,
    )
    for (let i = 0; i < 3; i += 1) frames2.shift()?.()
    assert.equal(target2.scrollLeft, 0, 'a zero speed must not move the board')
    stop2()
    void speed
  } finally {
    ;(globalThis as { window?: unknown }).window = realWindow
  }
})
