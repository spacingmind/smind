import { describe, expect, it } from 'vitest';
import { isAtBottom, nextFollowState } from '../transcript/stickToBottom';

const m = (contentOffsetY: number, contentHeight = 1000, layoutHeight = 500) => ({ contentOffsetY, contentHeight, layoutHeight });

describe('isAtBottom', () => {
  it('is true exactly at the threshold and false one pixel past it', () => {
    expect(isAtBottom(m(476))).toBe(true); // 1000 - (476 + 500) = 24
    expect(isAtBottom(m(475))).toBe(false); // 25
  });
  it('content shorter than the viewport is always at the bottom', () => {
    expect(isAtBottom({ contentOffsetY: 0, contentHeight: 100, layoutHeight: 500 })).toBe(true);
  });
  it('honours a custom threshold', () => {
    expect(isAtBottom(m(400), 100)).toBe(true);
    expect(isAtBottom(m(300), 100)).toBe(false);
  });
});

describe('nextFollowState', () => {
  it('scrolling up releases following', () => {
    expect(nextFollowState({ following: true }, m(100))).toEqual({ following: false });
  });
  it('scrolling back to the bottom re-engages', () => {
    expect(nextFollowState({ following: false }, m(500))).toEqual({ following: true });
  });
  it('growth with the viewport still at the old bottom reads as released only via a scroll event (offset unchanged, height grew)', () => {
    // a scroll event after growth with the same offset is a real distance > threshold: the caller only invokes this from onScroll
    expect(nextFollowState({ following: true }, m(500, 1000))).toEqual({ following: true });
  });
});
