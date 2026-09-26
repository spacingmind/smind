"use client"

import { useRef, useState } from "react"
import * as ResizablePrimitive from "react-resizable-panels"

import { cn } from "@/lib/utils"

/**
 * Matches paseo's resize-handle: the highlight only appears after a short
 * hover, so brushing past the handle on the way to something else doesn't
 * flicker it on.
 */
const HANDLE_HOVER_DELAY_MS = 150

function ResizablePanelGroup({
  className,
  ...props
}: ResizablePrimitive.GroupProps) {
  return (
    <ResizablePrimitive.Group
      data-slot="resizable-panel-group"
      className={cn(
        "flex h-full w-full aria-[orientation=vertical]:flex-col",
        className
      )}
      {...props}
    />
  )
}

function ResizablePanel({ ...props }: ResizablePrimitive.PanelProps) {
  return <ResizablePrimitive.Panel data-slot="resizable-panel" {...props} />
}

// Re-exported so callers driving a panel imperatively (collapse/expand,
// e.g. syncing the sidebar's panel width to its icon-collapsed state) pull
// the ref type from this wrapper like everything else here, rather than
// reaching past it into "react-resizable-panels" directly.
const usePanelRef = ResizablePrimitive.usePanelRef

function ResizableHandle({
  className,
  onPointerEnter,
  onPointerLeave,
  onPointerDown,
  onPointerUp,
  onPointerCancel,
  ...props
}: ResizablePrimitive.SeparatorProps) {
  const [highlighted, setHighlighted] = useState(false)
  const hoverTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearHoverTimer = () => {
    if (hoverTimerRef.current !== null) {
      clearTimeout(hoverTimerRef.current)
      hoverTimerRef.current = null
    }
  }

  return (
    <ResizablePrimitive.Separator
      data-slot="resizable-handle"
      data-highlighted={highlighted ? "" : undefined}
      onPointerEnter={(event) => {
        clearHoverTimer()
        hoverTimerRef.current = setTimeout(() => setHighlighted(true), HANDLE_HOVER_DELAY_MS)
        onPointerEnter?.(event)
      }}
      onPointerLeave={(event) => {
        clearHoverTimer()
        setHighlighted(false)
        onPointerLeave?.(event)
      }}
      onPointerDown={(event) => {
        clearHoverTimer()
        setHighlighted(true)
        onPointerDown?.(event)
      }}
      onPointerUp={(event) => {
        setHighlighted(false)
        onPointerUp?.(event)
      }}
      onPointerCancel={(event) => {
        setHighlighted(false)
        onPointerCancel?.(event)
      }}
      className={cn(
        // z-20 keeps the handle's hit area above shadcn's Sidebar, whose
        // fixed-position `sidebar-container` (z-10, see sidebar.tsx) sits
        // flush against this boundary -- without it, a real browser routes
        // pointerdown on the overlapping pixels to the sidebar instead of
        // this separator, and react-resizable-panels' own occlusion check
        // (correctly) refuses to treat that as a resize gesture, leaving a
        // dead zone along part of the drag handle.
        //
        // Matches ZCode's WorkspaceShellLayout resize handle (zcode-visual-
        // parity plan P2): the separator itself is a transparent 4px hit
        // area (`w-1`); the visible thing is a 2px `::after` indicator,
        // rounded at both ends, that only appears on hover (after the
        // delay above), focus, or an active drag -- never a permanent
        // line, so idle panel frames read as plain adjoining panels rather
        // than gridlines.
        "relative z-20 flex w-1 items-center justify-center bg-transparent ring-offset-background after:absolute after:inset-y-1 after:left-1/2 after:w-0.5 after:-translate-x-1/2 after:rounded-full after:bg-transparent after:transition-colors focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-hidden focus-visible:after:bg-foreground-subtlest/50 data-[highlighted]:after:bg-foreground-subtlest/50 aria-[orientation=horizontal]:h-1 aria-[orientation=horizontal]:w-full aria-[orientation=horizontal]:after:inset-x-1 aria-[orientation=horizontal]:after:inset-y-auto aria-[orientation=horizontal]:after:left-0 aria-[orientation=horizontal]:after:top-1/2 aria-[orientation=horizontal]:after:h-0.5 aria-[orientation=horizontal]:after:w-auto aria-[orientation=horizontal]:after:translate-x-0 aria-[orientation=horizontal]:after:-translate-y-1/2",
        className
      )}
      {...props}
    />
  )
}

export { ResizableHandle, ResizablePanel, ResizablePanelGroup, usePanelRef }
export type { PanelImperativeHandle } from "react-resizable-panels"
